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
        rehearse.EVIDENCE = None
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

    def test_pg_target_overrides_and_environment_indirection_rejected(self):
        allocation = {"host": "127.0.0.1", "port": 58421, "database": self.epoch["database"]}
        url = "postgres://postgres:synthetic@127.0.0.1:58421/" + allocation["database"]
        suffixes = ("?host=remote.invalid", "?port=5432", "?dbname=production", "?user=other",
                    "?service=production", "?hostaddr=192.0.2.1", "?sslmode=disable&host=remote.invalid",
                    "?sslmode=disable&sslmode=disable", "?%68ost=remote.invalid", "#host=remote.invalid")
        for target in (*[url + suffix for suffix in suffixes],
                       url.replace("127.0.0.1", "localhost"), url.replace(":58421", ":5432"),
                       url.replace("postgres:synthetic@", "other:synthetic@"),
                       url.replace(allocation["database"], "production"), url + "\n", "service=production"):
            with self.subTest(target=target), patch.dict(rehearse.os.environ, {"MIGRATE_DATABASE_URL": target}):
                with self.assertRaisesRegex(RuntimeError, "isolated_import_dsn"):
                    rehearse.importer_env(allocation)
        for suffix in ("", "?sslmode=disable"):
            with patch.dict(rehearse.os.environ, {"MIGRATE_DATABASE_URL": url + suffix,
                                                  "PGSERVICE": "production", "PGHOST": "remote.invalid",
                                                  "PGDATABASE": "production", "PGUSER": "other"}):
                actual = rehearse.importer_env(allocation)
                self.assertEqual(actual["MIGRATE_DATABASE_URL"], url + "?sslmode=disable")
                self.assertFalse(any(key.upper().startswith("PG") for key in actual))

    def prepared_guard_fixture(self):
        """Local hash fixtures only; these files do not assert successful CLI execution."""
        evidence = self.root / "evidence"
        runtime = evidence / "runtime"
        migration = self.write("evidence/runtime/platform/internal/store/migrations/090_final.sql", {"sql_fixture": True})
        self.write("evidence/runtime/tools/migrate/fixture.json", {})
        inventory = self.write("evidence/source-inventory.json", prepare.tree(runtime))
        ledger = self.write("evidence/migration-inventory.json", prepare.migration_inventory(runtime, migration.name))
        epoch = self.write("evidence/binding.json", {**self.epoch, "last_migration": migration.name})
        binary = self.write("evidence/runtime/dist/zns-migrate.exe", {"binary_hash_fixture": True})
        source = evidence / "source"
        for name in ("knowledge.yaml", "lineup.csv"):
            self.write("evidence/source/" + name, {"resource_hash_fixture": name})
            self.write("evidence/permanent/" + name, {"resource_hash_fixture": name})
        manifest = self.write("evidence/source/manifest.json", {
            "bot_id": 999, "snapshot_id": "synthetic-e-guard-fixture",
            "coverage": [{"domain": domain, "status": "included"} for domain in
                         ("users", "events", "orders", "passes", "massage", "messages", "knowledge", "schedule")],
            "files": [{"path": name, "kind": "resource", "source": domain,
                       "sha256": prepare.digest(source / name)} for name, domain in
                      (("knowledge.yaml", "knowledge"), ("lineup.csv", "schedule"))]})
        stage = evidence / "stage"
        self.write("evidence/stage/snapshot/manifest.json", prepare.read(manifest))
        prepared = self.write("evidence/prepared-import-bindings.json", {"source": prepare.tree(source),
                                                                        "stage": prepare.tree(stage)})
        config = {"runtime_source": str(runtime), "repository_root": str(self.root), "source_export": str(source),
                  "importer_binary": str(binary), "importer_sha256": prepare.digest(binary),
                  "source_inventory_sha256": prepare.digest(inventory), "migration_inventory_sha256": prepare.digest(ledger),
                  "binding_sha256": prepare.digest(epoch), "prepared_import_bindings_sha256": prepare.digest(prepared),
                  "imports": {}, "resources": {}}
        build = self.write("evidence/importer-build.json", {"commit": self.epoch["commit"],
                           "binary_sha256": config["importer_sha256"],
                           "source_inventory_sha256": config["source_inventory_sha256"],
                           "migration_inventory_sha256": config["migration_inventory_sha256"]})
        config["importer_build_sha256"] = prepare.digest(build)
        for domain in rehearse.DOMAINS:
            plan = self.write("evidence/" + domain + "-plan.json", {"plan_hash_fixture": domain})
            resolution = self.write("evidence/" + domain + "-resolutions.json", {"resolution_hash_fixture": domain})
            config["imports"][domain] = {"stage": str(stage), "plan": str(plan), "resolutions": str(resolution),
                                         "plan_sha256": prepare.digest(plan), "resolutions_sha256": prepare.digest(resolution)}
        for name in ("knowledge.yaml", "lineup.csv"):
            config["resources"][name] = {"source_path": name, "sha256": prepare.digest(source / name),
                                         "permanent_copy": str(evidence / "permanent" / name)}
        return config, evidence

    def test_actual_bound_bytes_rejected_before_any_database_or_executor(self):
        config, evidence = self.prepared_guard_fixture()
        rehearse.preflight(config, evidence)
        targets = [evidence / name for name in ("source-inventory.json", "migration-inventory.json", "binding.json",
                   "importer-build.json", "prepared-import-bindings.json", "source/manifest.json",
                   "source/knowledge.yaml", "source/lineup.csv", "stage/snapshot/manifest.json",
                   "permanent/knowledge.yaml", "permanent/lineup.csv")]
        targets += [Path(config["runtime_source"]) / "platform/internal/store/migrations/090_final.sql",
                    Path(config["importer_binary"])]
        targets += [Path(entry[key]) for entry in config["imports"].values() for key in ("plan", "resolutions")]
        for target in targets:
            original = target.read_bytes()
            target.write_bytes(original + b"changed")
            try:
                for action in (rehearse.import_all, rehearse.remove_receipts, rehearse.archive_importer):
                    with self.subTest(path=target.relative_to(evidence), action=action.__name__):
                        with patch.object(rehearse, "sql") as database, patch.object(rehearse, "command") as executor:
                            with self.assertRaises(RuntimeError):
                                action(config, evidence)
                            database.assert_not_called()
                            executor.assert_not_called()
            finally:
                target.write_bytes(original)

    def test_replay_requires_actual_food_reuse_and_other_counters(self):
        for summary in ({"reconciled": True}, {"reconciled": True, "reused": False},
                        {"reconciled": True, "applied": 0}, {"reconciled": True, "reused": 1}):
            with self.subTest(summary=summary), self.assertRaisesRegex(RuntimeError, "replay"):
                rehearse.validate_summary("food", summary, replay=True)
        rehearse.validate_summary("food", {"reconciled": True, "reused": True}, replay=True)
        for domain in set(rehearse.DOMAINS) - {"food"}:
            for applied in (None, False, 1, "0"):
                summary = {"reconciled": True}
                if applied is not None:
                    summary["applied"] = applied
                with self.subTest(domain=domain, summary=summary), self.assertRaisesRegex(RuntimeError, "replay"):
                    rehearse.validate_summary(domain, summary, replay=True)
            rehearse.validate_summary(domain, {"reconciled": True, "applied": 0}, replay=True)

    def test_checkpoint_and_offline_build_preflight_precedes_executor(self):
        config, evidence = self.prepared_guard_fixture()
        path = self.write("evidence/inputs.json", config)
        target = Path(config["imports"]["food"]["resolutions"])
        target.write_bytes(b"changed")
        for action in ("capture", "compare", "compile-probes"):
            argv = ["rehearse.py", action, "--inputs", str(path), "--inputs-sha256", prepare.digest(path),
                    "--evidence", str(evidence), "--allocation", "unused", "--allocation-sha256", "unused",
                    "--checkpoint", "checkpoint.json", "--probes", "unused", "--probes-sha256", "unused"]
            with self.subTest(action=action), patch.object(sys, "argv", argv):
                with (patch.object(rehearse, "allocate"), patch.object(rehearse, "sql") as database,
                      patch.object(rehearse, "command") as executor):
                    with self.assertRaisesRegex(RuntimeError, "reviewed_import_input_hash"):
                        rehearse.main()
                    database.assert_not_called()
                    executor.assert_not_called()

    def allocated_probe_fixture(self):
        """Complete local allocation bindings; no real execution receipt is asserted."""
        config, evidence = self.prepared_guard_fixture()
        epoch_path = evidence / "binding.json"
        epoch = prepare.read(epoch_path)
        epoch.update(last_migration="090_final.sql", status="frozen", reviewed_schema=True)
        epoch_path.write_text(json.dumps(epoch), encoding="utf-8")
        config["binding_sha256"] = prepare.digest(epoch_path)
        inputs = self.write("evidence/inputs.json", config)
        compose = self.write("compose.json", {"synthetic_fixture": True})
        images = self.write("images.json", {"synthetic_fixture": True})
        allocation = {**epoch, "transport": "host-loopback", "host": "127.0.0.1", "port": 58421,
                      "endpoint_verified": True, "writer": "e_rehearsal", "managed_stopped": True,
                      "managed_roles": ["zns_app", "zns_meter"], "input_reviewed": True,
                      "inputs_sha256": prepare.digest(inputs),
                      "runtime_source_inventory_sha256": config["source_inventory_sha256"],
                      "raw_migration_inventory_sha256": config["migration_inventory_sha256"],
                      "prerequisites_compose": str(compose), "prerequisites_compose_sha256": prepare.digest(compose),
                      "images_env": str(images), "images_env_sha256": prepare.digest(images),
                      "cli_image": "synthetic.invalid/cli@sha256:" + "a" * 64,
                      "config_volume": epoch["project"] + "-config"}
        projections = {}
        probes = {}
        inventory = {}
        for name in ("removal", "coverage", "sql"):
            projection = self.write(name + "-projection.json", {key: allocation[key]
                                    for key in ("database", "marker", "host", "port", "transport")})
            value = prepare.read(projection)
            value["role"] = "postgres" if name == "sql" else "zns_app"
            projection.write_text(json.dumps(value), encoding="utf-8")
            projections[name] = projection
            source = Path(__file__).parent / "probes" / name / "main.go"
            test = source.with_name("main_test.go")
            probes[name] = {"source": str(source), "sha256": prepare.digest(source),
                            "test_source": str(test), "test_sha256": prepare.digest(test),
                            "projection": str(projection), "projection_sha256": prepare.digest(projection)}
            for filename, original in (("main.go", source), ("main_test.go", test)):
                relative = "platform/cmd/e-" + name + "-probe/" + filename
                target = Path(config["runtime_source"]) / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                target.write_bytes(original.read_bytes())
                inventory[relative] = prepare.digest(original)
        manifest = self.write("probes.json", probes)
        inventory_path = self.write("evidence/probe-inventory.json", inventory)
        allocation.update(probes=str(manifest), probes_sha256=prepare.digest(manifest),
                          probe_inventory_sha256=prepare.digest(inventory_path))
        binary = Path(config["runtime_source"]) / "dist" / ("offline-sql.exe" if rehearse.os.name == "nt" else "offline-sql")
        binary.parent.mkdir(parents=True, exist_ok=True)
        binary.write_bytes(b"offline guard fixture; not an executable or real build proof")
        receipt = {"commit": epoch["commit"], "importer_absent": False,
                   "source_inventory_sha256": config["source_inventory_sha256"],
                   "migration_inventory_sha256": config["migration_inventory_sha256"],
                   "probes_sha256": allocation["probes_sha256"],
                   "probe_inventory_sha256": allocation["probe_inventory_sha256"],
                   "preconnection_tests": {"exit": 0},
                   "programs": {"sql": {"exit": 0, "sha256": prepare.digest(binary),
                               "command": ["go", "build", "-mod=readonly", "-o", str(binary), "./cmd/e-sql-probe"]}}}
        build = self.write("evidence/offline-runtime-build.json", receipt)
        allocation["sql_build_sha256"] = prepare.digest(build)
        allocation_path = self.write("allocation.json", allocation)
        return config, evidence, allocation_path, manifest, projections

    def test_bound_probe_mutation_blocks_every_owner_action(self):
        config, evidence, allocation, manifest, projections = self.allocated_probe_fixture()
        url = "postgres://postgres:synthetic@127.0.0.1:58421/" + self.epoch["database"]
        binary = Path(config["runtime_source"]) / "dist" / ("offline-sql.exe" if rehearse.os.name == "nt" else "offline-sql")
        for changed in (manifest, *projections.values(), binary, evidence / "offline-runtime-build.json"):
            original = changed.read_bytes()
            for action in ("import", "remove-receipts", "archive-importer", "capture", "compare", "build-runtime"):
                with self.subTest(changed=changed.name, action=action):
                    changed.write_bytes(original)
                    with patch.dict(rehearse.os.environ, {"MIGRATE_DATABASE_URL": url}):
                        rehearse.allocate(allocation, prepare.digest(allocation), config, evidence)
                        self.assertIsInstance(rehearse.preflight(config, evidence), dict)
                    changed.write_bytes(original + b" ")
                    argv = ["rehearse", action, "--inputs", str(evidence / "inputs.json"),
                            "--inputs-sha256", prepare.digest(evidence / "inputs.json"),
                            "--evidence", str(evidence), "--allocation", str(allocation),
                            "--allocation-sha256", prepare.digest(allocation), "--checkpoint", "checkpoint.json",
                            "--probes", str(manifest), "--probes-sha256", rehearse.ALLOCATION["probes_sha256"]]
                    with (patch.dict(rehearse.os.environ, {"MIGRATE_DATABASE_URL": url}),
                          patch.object(sys, "argv", argv), patch.object(rehearse, "allocate"),
                          patch.object(rehearse, "sql") as database, patch.object(rehearse, "command") as executor):
                        with self.assertRaisesRegex(RuntimeError, "reviewed_file_hash_mismatch"):
                            rehearse.main()
                        database.assert_not_called()
                        executor.assert_not_called()
            changed.write_bytes(original)

    def test_native_owner_argv_and_environment_follow_exact_port(self):
        config, evidence, path, manifest, projections = self.allocated_probe_fixture()
        allocation = prepare.read(path)
        commands = []
        for port in (25432, 25433):
            allocation["port"] = port
            probes = prepare.read(manifest)
            for name, projection in projections.items():
                value = prepare.read(projection)
                value["port"] = port
                projection.write_text(json.dumps(value), encoding="utf-8")
                probes[name]["projection_sha256"] = prepare.digest(projection)
            manifest.write_text(json.dumps(probes), encoding="utf-8")
            allocation["probes_sha256"] = prepare.digest(manifest)
            build_path = evidence / "offline-runtime-build.json"
            build = prepare.read(build_path)
            build["probes_sha256"] = allocation["probes_sha256"]
            build_path.write_text(json.dumps(build), encoding="utf-8")
            allocation["sql_build_sha256"] = prepare.digest(build_path)
            path.write_text(json.dumps(allocation), encoding="utf-8")
            url = "postgres://postgres:synthetic@127.0.0.1:" + str(port) + "/" + allocation["database"]
            with (patch.dict(rehearse.os.environ, {"MIGRATE_DATABASE_URL": url, "PGHOST": "other", "pgService": "other"}),
                  patch.object(rehearse, "command", return_value="safe-result\n") as executor):
                rehearse.allocate(path, prepare.digest(path), config, evidence)
                self.assertEqual(rehearse.sql("SELECT 1;"), "safe-result\n")
                argv = executor.call_args.args[0]
                env = executor.call_args.kwargs["env"]
                self.assertEqual(argv[1:], [str(projections["sql"]), prepare.digest(projections["sql"])])
                self.assertNotIn("synthetic@", " ".join(argv))
                self.assertFalse(any(key.upper().startswith("PG") for key in env))
                self.assertEqual(env["MIGRATE_DATABASE_URL"], url + "?sslmode=disable")
                self.assertEqual(executor.call_args.kwargs["data"], "SELECT 1;")
                commands.append(argv)
        self.assertNotEqual(commands[0], commands[1])

    def test_native_owner_binary_and_build_mutation_stop_before_execution(self):
        config, evidence, path, _, _ = self.allocated_probe_fixture()
        allocation = prepare.read(path)
        binary = Path(config["runtime_source"]) / "dist" / ("offline-sql.exe" if rehearse.os.name == "nt" else "offline-sql")
        url = "postgres://postgres:synthetic@127.0.0.1:58421/" + allocation["database"]
        for changed in (binary, evidence / "offline-runtime-build.json"):
            original = changed.read_bytes()
            with (patch.dict(rehearse.os.environ, {"MIGRATE_DATABASE_URL": url}),
                  patch.object(rehearse, "command") as executor):
                rehearse.allocate(path, prepare.digest(path), config, evidence)
                changed.write_bytes(original + b" ")
                with self.assertRaisesRegex(RuntimeError, "reviewed_file_hash_mismatch"):
                    rehearse.sql("SELECT 1;")
                with self.assertRaisesRegex(RuntimeError, "reviewed_file_hash_mismatch"):
                    rehearse.preflight(config, evidence)
                executor.assert_not_called()
            changed.write_bytes(original)

    def test_owner_localhost_is_rejected_before_executor(self):
        config, evidence, path, _, _ = self.allocated_probe_fixture()
        allocation = prepare.read(path)
        allocation["host"] = "localhost"
        path.write_text(json.dumps(allocation), encoding="utf-8")
        with patch.object(rehearse, "command") as executor:
            with self.assertRaisesRegex(RuntimeError, "exclusive_reviewed_operator_allocation_required"):
                rehearse.allocate(path, prepare.digest(path), config, evidence)
            executor.assert_not_called()

    def test_runtime_projection_binds_exact_allocation(self):
        allocation = {"database": "synthetic_qa_zns_guard", "marker": prepare.SCOPE,
                      "host": "127.0.0.1", "port": 25432, "transport": "host-loopback"}
        value = dict(allocation, role="zns_app")
        path = self.write("projection.json", value)
        entry = {"projection": str(path), "projection_sha256": prepare.digest(path)}
        self.assertEqual(rehearse.probe_projection(entry, allocation, allocation), value)
        for key, wrong in (("host", "localhost"), ("port", 25433), ("database", "synthetic_qa_zns_copy"),
                           ("marker", "other"), ("transport", "network"), ("role", "postgres")):
            with self.subTest(key=key):
                path.write_text(json.dumps(dict(value, **{key: wrong})), encoding="utf-8")
                entry["projection_sha256"] = prepare.digest(path)
                with self.assertRaises(RuntimeError):
                    rehearse.probe_projection(entry, allocation, allocation)

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
