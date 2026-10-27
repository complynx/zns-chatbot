#!/usr/bin/env python3
"""Export the pinned provider and apply the owned prelink patch without changing it.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
"""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import tempfile

BASE_COMMIT = "59ac6a152e04dd6af0b10968dc67b10126921bb6"
EXPORT_PATHS = ("go.mod", "go.sum", "cmd", "internal", "LICENSE")
NEW_FILES = {"internal/app/prelink.go", "internal/app/prelink_test.go",
             "internal/app/prelink_config_test.go"}


def git(directory, *args):
    # Caller Git overrides must not redirect this command into another index/tree.
    env = {k: v for k, v in os.environ.items() if not k.upper().startswith("GIT_")}
    result = subprocess.run(["git", "--no-replace-objects", "-c", "core.autocrlf=false", "-C", str(directory), *args], env=env,
                            capture_output=True, check=True)
    return result.stdout


def digest(data):
    return hashlib.sha256(data).hexdigest()


def inventory(directory):
    result = {}
    for path in sorted(directory.rglob("*")):
        if path.is_symlink():
            raise ValueError("source must not contain symlinks")
        if path.is_file():
            result[path.relative_to(directory).as_posix()] = digest(path.read_bytes())
    return result


def prepare(repository, output):
    root = Path(__file__).resolve().parents[3]
    owned_root = (root / ".cache/shuttle-live-test/zitadeltg-runtime").resolve()
    output = output.resolve()
    if not output.is_relative_to(owned_root) or output == owned_root:
        raise ValueError("output must be a new directory inside the owned runtime cache")
    receipt_path = output.with_name(output.name + "-receipt.json")
    if output.exists() or receipt_path.exists():
        raise ValueError("output or receipt already exists; choose a new directory")
    patch = Path(__file__).with_name("zitadeltg-prelink.patch").resolve()
    patch_bytes = patch.read_bytes()
    entries = git(repository, "ls-tree", "-r", "-z", "--full-tree", BASE_COMMIT,
                  "--", *EXPORT_PATHS).split(b"\0")
    with tempfile.TemporaryDirectory(prefix="zinusya-provider-") as temporary:
        work = Path(temporary).resolve()
        # Bound recursive TemporaryDirectory cleanup to the generated temp directory.
        if work.parent != Path(tempfile.gettempdir()).resolve() or not work.name.startswith("zinusya-provider-"):
            raise ValueError("unexpected temporary directory")
        for entry in filter(None, entries):
            metadata, raw_name = entry.split(b"\t", 1)
            mode, kind, object_id = metadata.decode("ascii").split()
            name = raw_name.decode("utf-8")
            relative = PurePosixPath(name)
            if mode != "100644" or kind != "blob" or relative.is_absolute() or ".." in relative.parts or "\\" in name:
                raise ValueError("unsupported export entry")
            target = work.joinpath(*relative.parts)
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(git(repository, "cat-file", "blob", object_id))
        base_files = inventory(work)
        if not {"go.mod", "go.sum", "LICENSE"}.issubset(base_files):
            raise ValueError("incomplete provider export")
        git(work, "apply", "--check", "--whitespace=error-all", str(patch))
        git(work, "apply", "--whitespace=error-all", str(patch))
        files = inventory(work)
        if set(files) != set(base_files) | NEW_FILES:
            raise ValueError("unexpected patched source inventory")
        output.mkdir(parents=True, exist_ok=False)
        for name in files:
            target = output / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes((work / name).read_bytes())
        receipt = {"commit": BASE_COMMIT, "patch_sha256": digest(patch_bytes),
                   "base_files": base_files, "files": files}
        with receipt_path.open("x", encoding="utf-8", newline="\n") as stream:
            json.dump(receipt, stream, indent=2)
            stream.write("\n")
    print(f"Prepared {len(files)} files; receipt: {receipt_path}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path, required=True,
                        help="read-only sibling zitadeltg Git repository")
    parser.add_argument("--output", type=Path, required=True,
                        help="new directory inside .cache/shuttle-live-test/zitadeltg-runtime")
    args = parser.parse_args()
    prepare(args.repository.resolve(), args.output)


if __name__ == "__main__":
    main()
