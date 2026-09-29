"""Inspect a local XLSX evidence file without evaluating formulas or extracting ZIPs."""

import argparse
import json
import posixpath
import re
import sys
import zipfile
from pathlib import Path
from urllib.parse import unquote, urlsplit
import xml.etree.ElementTree as ET

MAX_ARCHIVE = 32 * 1024 * 1024
MAX_EXPANDED = 64 * 1024 * 1024
MAX_PART = 32 * 1024 * 1024
MAX_MEMBERS = 2048
MAX_CELLS = 1_000_000
SHEET_NS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
REL_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
PACKAGE_NS = "http://schemas.openxmlformats.org/package/2006/relationships"
ESCAPE = re.compile(r"_x([0-9a-fA-F]{4})_")
CELL_REF = re.compile(r"([A-Z]{1,3})([1-9][0-9]{0,6})\Z")


class InspectionError(ValueError):
    """The input is unsafe, excessive, or outside the supported XLSX structure."""


class BoundedTree(ET.TreeBuilder):
    def __init__(self):
        super().__init__()
        self.depth = 0
        self.nodes = 0

    def doctype(self, name, pubid, system):
        raise InspectionError("XML document types and entities are not supported")

    def start(self, tag, attrs):
        self.depth += 1
        self.nodes += 1
        if self.depth > 64 or self.nodes > 2_000_000:
            raise InspectionError("XML nesting or node limit exceeded")
        return super().start(tag, attrs)

    def end(self, tag):
        self.depth -= 1
        return super().end(tag)


def decode_text(value):
    """Decode one OOXML escape pass; escaped underscores protect literal tokens."""
    value = ESCAPE.sub(lambda match: chr(int(match[1], 16)), value)
    return value.encode("utf-16-le", "surrogatepass").decode("utf-16-le", "surrogatepass")


def string_text(element):
    if element is None:
        return ""
    # Rich text runs are concatenated; phonetic annotations are not cell text.
    text = "".join(t.text or "" for t in element.findall(f"{{{SHEET_NS}}}t"))
    text += "".join(t.text or "" for t in element.findall(f"{{{SHEET_NS}}}r/{{{SHEET_NS}}}t"))
    return decode_text(text)


def part_target(source, target):
    parsed = urlsplit(target)
    if parsed.scheme or parsed.netloc or parsed.query or parsed.fragment:
        raise InspectionError("Relationship target is not a local package part")
    target = unquote(parsed.path)
    if not target or "\\" in target or "\x00" in target:
        raise InspectionError("Unsafe relationship target")
    resolved = posixpath.normpath(posixpath.join(posixpath.dirname(source), target))
    if target.startswith("/"):
        resolved = posixpath.normpath(target.lstrip("/"))
    if resolved == ".." or resolved.startswith("../") or resolved in ("", "."):
        raise InspectionError("Relationship escapes package root")
    return resolved


def inspect_workbook(path):
    """Return sparse worksheet cells with original coordinates and raw value types."""
    path = Path(path)
    if not path.is_file() or path.stat().st_size > MAX_ARCHIVE:
        raise InspectionError("Expected a local file no larger than 32 MiB")
    with zipfile.ZipFile(path) as archive:
        members = archive.infolist()
        if len(members) > MAX_MEMBERS or sum(m.file_size for m in members) > MAX_EXPANDED:
            raise InspectionError("ZIP member count or expanded size limit exceeded")
        names = set()
        for member in members:
            name = member.filename
            if (name in names or name.startswith("/") or "\\" in name
                    or ".." in name.split("/") or "\x00" in name
                    or member.flag_bits & 1 or member.file_size > MAX_PART
                    or member.compress_type not in (zipfile.ZIP_STORED, zipfile.ZIP_DEFLATED)):
                raise InspectionError("Unsafe, duplicate, encrypted, or excessive ZIP member")
            names.add(name)

        parsed_bytes = 0

        def xml(name):
            nonlocal parsed_bytes
            if name not in names:
                raise InspectionError(f"Missing package part: {name}")
            with archive.open(name) as stream:
                data = stream.read(MAX_PART + 1)
            if len(data) > MAX_PART:
                raise InspectionError("Expanded XML part size limit exceeded")
            parsed_bytes += len(data)
            if parsed_bytes > MAX_EXPANDED:
                raise InspectionError("Cumulative XML parsing size limit exceeded")
            return ET.fromstring(data, parser=ET.XMLParser(target=BoundedTree()))

        def relationships(source):
            folder, base = posixpath.split(source)
            name = posixpath.join(folder, "_rels", base + ".rels") if source else "_rels/.rels"
            root = xml(name)
            if root.tag != f"{{{PACKAGE_NS}}}Relationships":
                raise InspectionError("Unsupported relationship namespace")
            result = {}
            for rel in root:
                key = rel.get("Id")
                if not key or key in result or rel.get("TargetMode", "Internal") != "Internal":
                    raise InspectionError("Duplicate or external relationship")
                result[key] = (rel.get("Type", ""), part_target(source, rel.get("Target", "")))
            return result

        office = [target for kind, target in relationships("").values()
                  if kind == REL_NS + "/officeDocument"]
        if len(office) != 1:
            raise InspectionError("Expected one XLSX workbook relationship")
        workbook = xml(office[0])
        if workbook.tag != f"{{{SHEET_NS}}}workbook":
            raise InspectionError("Unsupported workbook namespace")
        rels = relationships(office[0])
        shared_parts = [target for kind, target in rels.values() if kind == REL_NS + "/sharedStrings"]
        if len(shared_parts) > 1:
            raise InspectionError("Multiple shared string parts")
        shared = [string_text(item) for item in xml(shared_parts[0])] if shared_parts else []
        result = {"sheets": []}
        count = 0
        sheet_names = set()
        for sheet in workbook.findall(f"{{{SHEET_NS}}}sheets/{{{SHEET_NS}}}sheet"):
            name = decode_text(sheet.get("name", ""))
            if not name or name in sheet_names:
                raise InspectionError("Missing or duplicate sheet name")
            sheet_names.add(name)
            kind, target = rels.get(sheet.get(f"{{{REL_NS}}}id"), ("", ""))
            if kind != REL_NS + "/worksheet":
                raise InspectionError("Missing or unsupported worksheet relationship")
            root = xml(target)
            if root.tag != f"{{{SHEET_NS}}}worksheet":
                raise InspectionError("Unsupported worksheet namespace")
            cells = []
            coordinates = set()
            for cell in root.findall(f"{{{SHEET_NS}}}sheetData/{{{SHEET_NS}}}row/{{{SHEET_NS}}}c"):
                count += 1
                ref = cell.get("r", "")
                match = CELL_REF.fullmatch(ref)
                column = 0
                if match:
                    for char in match[1]:
                        column = column * 26 + ord(char) - ord("A") + 1
                if (count > MAX_CELLS or not match or column > 16384
                        or int(match[2]) > 1048576 or ref in coordinates):
                    raise InspectionError("Invalid/duplicate cell coordinate or cell limit exceeded")
                coordinates.add(ref)
                cell_type = cell.get("t", "n")
                raw = cell.findtext(f"{{{SHEET_NS}}}v", "")
                if cell_type == "s":
                    if not raw.isascii() or not raw.isdecimal() or int(raw) >= len(shared):
                        raise InspectionError("Invalid shared string index")
                    text = shared[int(raw)]
                elif cell_type == "inlineStr":
                    text = string_text(cell.find(f"{{{SHEET_NS}}}is"))
                else:
                    text = decode_text(raw) if cell_type == "str" else raw
                formula = cell.find(f"{{{SHEET_NS}}}f")
                cells.append({"ref": ref, "type": cell_type, "text": text,
                              "formula": None if formula is None else {
                                  "text": formula.text or "", "attributes": dict(formula.attrib)}})
            result["sheets"].append({"name": name, "part": target, "cells": cells,
                                     "panes": [dict(p.attrib) for p in root.findall(f".//{{{SHEET_NS}}}pane")],
                                     "filters": [dict(f.attrib) for f in root.findall(f"{{{SHEET_NS}}}autoFilter")]})
        return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("file", type=Path, help="Local .xlsx evidence file")
    args = parser.parse_args()
    try:
        result = inspect_workbook(args.file)
    except (InspectionError, OSError, zipfile.BadZipFile, ET.ParseError, ValueError) as error:
        print(f"Workbook inspection failed: {error}", file=sys.stderr)
        return 1
    print(json.dumps(result, ensure_ascii=True, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
