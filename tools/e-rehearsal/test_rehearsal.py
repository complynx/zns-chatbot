"""Focused offline guard tests. No successful CLI receipt is mocked."""
import json
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import prepare
import rehearse


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        rehearse.ALLOCATION = None
        self.epoch = {"commit": "a" * 40, "last_migration": "089_observation.sql",
                      "database": "synthetic_qa_zns_guard", "project": "synthetic-qa-zns-guard",
                      "marker": prepare.SCOPE, "status": "offline", "owner": "guard-test"}

    def write(self, name, value):
        target = self.root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(json.dumps(value), encoding="utf-8")
        return target

    def test_unbound_epoch_database_marker_and_final_epoch(self):
        for key, value in (("commit", "main"), ("database", "production"), ("marker", "old"),
                           ("last_migration", "088.sql"), ("status", "preparation_not_frozen")):
            with self.subTest(key=key):
                path = self.write("binding.json", {**self.epoch, key: value})
                with self.assertRaises(RuntimeError):
                    prepare.binding(path, prepare.digest(path))
        path = self.write("binding.json", {**self.epoch, "status": "frozen", "reviewed_schema": True})
        with self.assertRaisesRegex(RuntimeError, "reviewed_final_epoch"):
            prepare.binding(path, prepare.digest(path), frozen=True)

    def test_changed_reviewed_file_rejected(self):
        path = self.write("binding.json", self.epoch)
        sha = prepare.digest(path)
        path.write_text("{}", encoding="utf-8")
        with self.assertRaises(RuntimeError):
            prepare.checked(path, sha)

    def test_outside_and_absolute_paths_rejected(self):
        for name in ("../outside", "/root/file", "C:/root/file", "nested\\file"):
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                prepare.safe_path(self.root, name)

    def test_linked_path_rejected_before_resolution(self):
        with patch.object(Path, "is_symlink", return_value=True):
            with self.assertRaisesRegex(RuntimeError, "linked"):
                prepare.safe_path(self.root, "linked/file")

    def test_exact_inventory_rejects_changed_missing_extra_files(self):
        path = self.write("platform/source.json", {"version": 1})
        expected = {"platform/source.json": prepare.digest(path)}
        prepare.verify_source(self.root, expected)
        path.write_bytes(b"changed")
        with self.assertRaises(RuntimeError):
            prepare.verify_source(self.root, expected)
        path.unlink()
        with self.assertRaises(RuntimeError):
            prepare.verify_source(self.root, expected)
        self.write("platform/unlisted.json", {})
        with self.assertRaises(RuntimeError):
            prepare.verify_source(self.root, expected)

    def test_raw_sql_line_endings_and_migration_tail(self):
        path = self.root / "platform/internal/store/migrations/089_observation.sql"
        path.parent.mkdir(parents=True)
        path.write_bytes(b"SELECT 1;\n")
        initial = prepare.migration_inventory(self.root, path.name)
        path.write_bytes(b"SELECT 1;\r\n")
        changed = prepare.migration_inventory(self.root, path.name)
        self.assertNotEqual(initial, changed)
        with self.assertRaisesRegex(RuntimeError, "migration_epoch"):
            prepare.migration_inventory(self.root, "090_not_present.sql")

    def test_git_export_preserves_blobs_and_rejects_export_ignored_source(self):
        repository = self.root / "repository"
        repository.mkdir()
        prepare.command(["git", "init", "--quiet"], cwd=repository)
        sql = repository / "platform/internal/store/migrations/089_observation.sql"
        sql.parent.mkdir(parents=True)
        sql.write_bytes(b"SELECT 1;\r\n")
        importer = repository / "tools/migrate/source.json"
        importer.parent.mkdir(parents=True)
        importer.write_text('{"guard_fixture":true}', encoding="utf-8")
        attributes = repository / ".gitattributes"
        attributes.write_text("*.sql text eol=lf\n", encoding="utf-8")
        def commit():
            prepare.command(["git", "add", "."], cwd=repository)
            prepare.command(["git", "-c", "user.name=E guard fixture", "-c", "user.email=qa@synthetic.invalid",
                             "commit", "--quiet", "-m", "test: synthetic source guard fixture",
                             "-m", "Written by E capability developer (gpt-6/Codex)\non behalf of Daniel Drizhuk"],
                            cwd=repository)
            return {"commit": prepare.command(["git", "rev-parse", "HEAD"], cwd=repository).strip()}
        epoch = commit()
        exported = self.root / "exported"
        exported.mkdir()
        inventory = prepare.archive_source(repository, epoch, exported)
        self.assertEqual((exported / sql.relative_to(repository)).read_bytes(), b"SELECT 1;\n")
        self.assertIn(sql.relative_to(repository).as_posix(), inventory)
        attributes.write_text("*.sql text eol=lf export-ignore\n", encoding="utf-8")
        omitted = self.root / "omitted"
        omitted.mkdir()
        with self.assertRaisesRegex(RuntimeError, "complete_tracked_source"):
            prepare.archive_source(repository, commit(), omitted)

    def test_unfrozen_allocation_rejected_before_executor(self):
        path = self.write("allocation.json", self.epoch)
        with patch.object(rehearse, "command") as executor:
            with self.assertRaisesRegex(RuntimeError, "reviewed_final_epoch"):
                rehearse.allocate(path, prepare.digest(path), {}, self.root)
            executor.assert_not_called()
        with patch.object(rehearse, "command") as executor:
            with self.assertRaisesRegex(RuntimeError, "reviewed_allocation"):
                rehearse.sql("DROP SCHEMA migrate_import")
            executor.assert_not_called()

    def test_archive_cannot_target_checkout(self):
        config = {"runtime_source": str(self.root), "repository_root": str(self.root)}
        with self.assertRaisesRegex(RuntimeError, "isolated_build_source"):
            rehearse.build_runtime(config, self.root, "absent", "a" * 64, retired=False)

    def test_wrong_transport_writer_inputs_and_source_bindings_rejected(self):
        epoch = {**self.epoch, "last_migration": "090_final.sql", "status": "frozen",
                 "reviewed_schema": True}
        binding_path = self.write("binding.json", epoch)
        inputs_path = self.write("inputs.json", {"guard_fixture": True})
        config = {"binding_sha256": prepare.digest(binding_path),
                  "source_inventory_sha256": "c" * 64, "migration_inventory_sha256": "d" * 64}
        allocation = {**epoch, "transport": "host-loopback", "host": "127.0.0.1", "port": 58421,
                      "endpoint_verified": True, "writer": "e_rehearsal",
                      "managed_roles": ["zns_app", "zns_meter"], "managed_stopped": True,
                      "inputs_sha256": prepare.digest(inputs_path), "input_reviewed": True,
                      "runtime_source_inventory_sha256": "c" * 64,
                      "raw_migration_inventory_sha256": "d" * 64}
        for key, value in (("transport", "owned-network"), ("host", "remote.invalid"),
                           ("writer", "another_writer"), ("managed_stopped", False),
                           ("input_reviewed", False), ("inputs_sha256", "e" * 64),
                           ("raw_migration_inventory_sha256", "e" * 64)):
            with self.subTest(key=key), patch.object(rehearse, "command") as executor:
                path = self.write("allocation.json", {**allocation, key: value})
                with self.assertRaisesRegex(RuntimeError, "exclusive_reviewed_operator"):
                    rehearse.allocate(path, prepare.digest(path), config, self.root)
                executor.assert_not_called()

    def test_failed_command_does_not_print_private_error(self):
        with self.assertRaisesRegex(RuntimeError, "^e_command_failed$"):
            prepare.command([sys.executable, "-c",
                             "import sys; sys.stderr.write('private-token'); sys.exit(1)"])

    def test_save_does_not_overwrite_evidence(self):
        path = self.root / "receipt.json"
        prepare.save(path, {"existing": True})
        with self.assertRaises(FileExistsError):
            prepare.save(path, {"existing": False})
        self.assertEqual(prepare.read(path), {"existing": True})

    def test_existing_evidence_rejected_before_build_or_cli(self):
        evidence = self.root / "qa.local/existing"
        evidence.mkdir(parents=True)
        existing = evidence / "earlier.json"
        existing.write_bytes(b"preserved")
        args = SimpleNamespace(repository=str(self.root), package=str(self.root),
                               binding="unused", binding_sha256="unused",
                               package_inventory="unused", package_inventory_sha256="unused",
                               evidence=str(evidence))
        with (patch.object(prepare, "binding", return_value=self.epoch),
              patch.object(prepare, "verify_package", return_value=self.root),
              patch.object(prepare, "archive_source") as executor):
            with self.assertRaises(FileExistsError):
                prepare.prepare(args)
            executor.assert_not_called()
        self.assertEqual(existing.read_bytes(), b"preserved")


if __name__ == "__main__":
    unittest.main()
