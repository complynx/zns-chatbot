"""Independent, synthetic package tests for the evidence inspector."""

import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from zipfile import ZIP_DEFLATED, ZipFile

from inspect_workbook import InspectionError, inspect_workbook

S = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
P = "http://schemas.openxmlformats.org/package/2006/relationships"


def package_parts():
    return {
        "_rels/.rels": f'<Relationships xmlns="{P}"><Relationship Id="book" Type="{R}/officeDocument" Target="custom/book.xml"/></Relationships>',
        "custom/book.xml": f'<workbook xmlns="{S}" xmlns:r="{R}"><sheets><sheet name="雪" sheetId="7" r:id="tab"/></sheets></workbook>',
        "custom/_rels/book.xml.rels": f'<Relationships xmlns="{P}"><Relationship Id="tab" Type="{R}/worksheet" Target="../tabs/unusual.xml"/><Relationship Id="strings" Type="{R}/sharedStrings" Target="words.xml"/></Relationships>',
        "custom/words.xml": f'<sst xmlns="{S}"><si><r><t>Привет </t></r><r><t>_xD83D__xDE80_</t></r><rPh sb="0" eb="1"><t>not cell text</t></rPh></si><si><t>_x005F_x0041_</t></si></sst>',
        "tabs/unusual.xml": f'''<worksheet xmlns="{S}">
          <sheetViews><sheetView><pane state="frozen" ySplit="1" topLeftCell="A2"/></sheetView></sheetViews>
          <sheetData><row r="1">
            <c r="A1" t="s"><v>0</v></c>
            <c r="C1" t="inlineStr"><is><t>=SUM(1,2) &amp; 雪_x000D_&#10;_x005F_x0041_</t></is></c>
            <c r="D1"><f t="shared" si="0">SUM(A2:B2)</f><v>3</v></c>
            <c r="F1" t="s"><v>1</v></c>
          </row><row r="100"><c r="XFD100"/></row></sheetData>
          <autoFilter ref="A1:F100"/>
        </worksheet>''',
    }


class WorkbookInspectorTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / "evidence.xlsx"

    def write(self, parts):
        with ZipFile(self.path, "w", ZIP_DEFLATED) as archive:
            for name, text in parts.items():
                archive.writestr(name, text)

    def test_relationships_text_formulas_and_sparse_coordinates(self):
        self.write(package_parts())
        sheet = inspect_workbook(self.path)["sheets"][0]
        self.assertEqual((sheet["name"], sheet["part"]), ("雪", "tabs/unusual.xml"))
        cells = sheet["cells"]
        self.assertEqual([c["ref"] for c in cells], ["A1", "C1", "D1", "F1", "XFD100"])
        self.assertEqual(cells[0]["text"], "Привет 🚀")
        self.assertEqual(cells[1]["text"], "=SUM(1,2) & 雪\r\n_x0041_")
        self.assertIsNone(cells[1]["formula"])
        self.assertEqual(cells[2]["formula"], {"text": "SUM(A2:B2)", "attributes": {"t": "shared", "si": "0"}})
        self.assertEqual(cells[2]["text"], "3")
        self.assertEqual(cells[3]["text"], "_x0041_")
        self.assertEqual(cells[4]["text"], "")
        self.assertEqual(sheet["panes"][0]["state"], "frozen")
        self.assertEqual(sheet["filters"], [{"ref": "A1:F100"}])

    def test_no_shared_string_table_needed_for_inline_cells(self):
        parts = package_parts()
        parts["custom/_rels/book.xml.rels"] = f'<Relationships xmlns="{P}"><Relationship Id="tab" Type="{R}/worksheet" Target="/tabs/unusual.xml"/></Relationships>'
        parts["tabs/unusual.xml"] = f'<worksheet xmlns="{S}"><sheetData><row><c r="B9" t="inlineStr"><is><t>text</t></is></c></row></sheetData></worksheet>'
        self.write(parts)
        self.assertEqual(inspect_workbook(self.path)["sheets"][0]["cells"][0]["text"], "text")

    def test_rejects_external_and_root_escape_relationships(self):
        for target in ["https://example.invalid/book.xml", "../../book.xml", "%2e%2e/book.xml"]:
            with self.subTest(target=target):
                parts = package_parts()
                parts["_rels/.rels"] = f'<Relationships xmlns="{P}"><Relationship Id="book" Type="{R}/officeDocument" Target="{target}"/></Relationships>'
                self.write(parts)
                with self.assertRaises(InspectionError):
                    inspect_workbook(self.path)

    def test_rejects_doctype_in_utf8_and_utf16(self):
        for encoding in ["utf-8", "utf-16"]:
            with self.subTest(encoding=encoding):
                parts = package_parts()
                parts["custom/book.xml"] = ('<!DOCTYPE workbook [<!ENTITY x "bad">]>' + parts["custom/book.xml"]).encode(encoding)
                self.write(parts)
                with self.assertRaises(InspectionError):
                    inspect_workbook(self.path)

    def test_rejects_traversal_and_limits(self):
        parts = package_parts()
        parts["../unsafe"] = "unused"
        self.write(parts)
        with self.assertRaises(InspectionError):
            inspect_workbook(self.path)
        self.write(package_parts())
        for limit in ["MAX_ARCHIVE", "MAX_EXPANDED", "MAX_PART", "MAX_MEMBERS", "MAX_CELLS"]:
            with self.subTest(limit=limit), patch("inspect_workbook." + limit, 1):
                with self.assertRaises(InspectionError):
                    inspect_workbook(self.path)

    def test_rejects_invalid_string_index_and_duplicate_coordinates(self):
        for replacement in ['<c r="A1" t="s"><v>-1</v></c>', '<c r="A1"/><c r="A1"/>', '<c r="XFE1"/>']:
            with self.subTest(replacement=replacement):
                parts = package_parts()
                parts["tabs/unusual.xml"] = f'<worksheet xmlns="{S}"><sheetData><row>{replacement}</row></sheetData></worksheet>'
                self.write(parts)
                with self.assertRaises(InspectionError):
                    inspect_workbook(self.path)


if __name__ == "__main__":
    unittest.main()
