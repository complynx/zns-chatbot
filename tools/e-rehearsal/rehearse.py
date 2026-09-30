"""Allocated synthetic E checks. No operation runs when this module is imported."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
from urllib.parse import urlparse

from prepare import (SCOPE, binding, checked, clean_build_env, migration_inventory,
                     read, safe_path, verify_source)

DATABASE = None
PROJECT = None
ALLOCATION = None
DOMAINS = ("users", "events", "orders", "passes", "food", "massage", "messages")
RECEIPTS = ("user_receipts", "event_receipts", "order_receipts", "pass_receipts",
            "food_receipts", "massage_receipts", "message_receipts")
REQUIRED = ("core.legacy_user_references", "core.legacy_event_references",
            "core.legacy_order_import_references", "core.legacy_pass_import_references",
            "core.legacy_food_import_references", "core.legacy_massage_import_references",
            "core.legacy_message_references", "core.conversation_events",
            "core.conversation_message_bodies", "core.legacy_massage_drafts",
            "core.massage_bookings", "core.order_proofs", "public.zns_schema_migrations")


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def tree(path):
    root = Path(path).resolve(strict=True)
    entries = {}
    for item in sorted(root.rglob("*")):
        if item.is_symlink():
            raise RuntimeError("symlink_in_evidence")
        safe_path(root, item.relative_to(root).as_posix())
        if item.is_file():
            entries[item.relative_to(root).as_posix()] = digest(item)
    return entries


def save(path, value):
    with Path(path).open("x", encoding="utf-8") as output:
        json.dump(value, output, ensure_ascii=False, indent=2, sort_keys=True)


def command(argv, *, cwd=None, data=None, env=None):
    result = subprocess.run(argv, cwd=cwd, input=data, env=env, text=True,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=300, check=False)
    if result.returncode:
        # Never emit captured stderr, DSNs or imported private data.
        raise RuntimeError("root_command_failed")
    return result.stdout


def sql(query):
    if ALLOCATION is None:
        raise RuntimeError("reviewed_allocation_required")
    return command(["docker", "--context", "desktop-linux", "compose", "-p", PROJECT,
                    "-f", ALLOCATION["prerequisites_compose"],
                    "--env-file", ALLOCATION["images_env"], "exec", "-T", "postgres",
                    "psql", "-X", "-A", "-t", "-U", "postgres", "-d", DATABASE,
                    "-v", "ON_ERROR_STOP=1"], data=query)


def guard():
    result = sql("SELECT json_build_object('database',current_database(),"
                 "'owner',current_user,'busy',(SELECT count(*) FROM pg_stat_activity "
                 "WHERE datname=current_database() AND usename IN('zns_app','zns_meter')));\n")
    record = json.loads(result)
    if record != {"database": DATABASE, "owner": "postgres", "busy": 0}:
        raise RuntimeError("exclusive_synthetic_database_required")
    if sql("SELECT description FROM pg_shdescription WHERE "
           "objoid=(SELECT oid FROM pg_database WHERE datname=current_database()) "
           "AND classoid='pg_database'::regclass;\n").strip() != SCOPE:
        raise RuntimeError("synthetic_ownership_marker_required")


def allocate(path, sha, config, evidence):
    global DATABASE, PROJECT, ALLOCATION
    value = binding(path, sha, frozen=True)
    epoch = read(checked(evidence / "binding.json", config["binding_sha256"]))
    for key in ("commit", "last_migration", "database", "project", "marker", "owner"):
        if value[key] != epoch[key]:
            raise RuntimeError("allocation_preparation_epoch_mismatch")
    if (value.get("transport") != "host-loopback" or value.get("host") not in ("127.0.0.1", "localhost")
            or not isinstance(value.get("port"), int) or not 1024 <= value["port"] <= 65535
            or value.get("endpoint_verified") is not True or value.get("writer") != "e_rehearsal"
            or value.get("managed_roles") != ["zns_app", "zns_meter"]
            or value.get("managed_stopped") is not True
            or value.get("inputs_sha256") != digest(evidence / "inputs.json")
            or value.get("runtime_source_inventory_sha256") != config["source_inventory_sha256"]
            or value.get("raw_migration_inventory_sha256") != config["migration_inventory_sha256"]
            or value.get("input_reviewed") is not True):
        raise RuntimeError("exclusive_reviewed_operator_allocation_required")
    for name in ("prerequisites_compose", "images_env"):
        checked(value[name], value[name + "_sha256"])
    checked(evidence / "probe-inventory.json", value["probe_inventory_sha256"])
    if (not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_./:-]*@sha256:[0-9a-f]{64}", value.get("cli_image", ""))
            or not re.fullmatch(re.escape(value["project"]) + r"-[a-z0-9-]+", value.get("config_volume", ""))):
        raise RuntimeError("allocated_immutable_resources_required")
    parsed = urlparse(os.environ.get("MIGRATE_DATABASE_URL", ""))
    if (parsed.scheme not in ("postgres", "postgresql") or parsed.hostname != value["host"] or parsed.port != value["port"]
            or parsed.path != "/" + value["database"] or parsed.username != "postgres"):
        raise RuntimeError("isolated_import_dsn_required")
    DATABASE, PROJECT, ALLOCATION = value["database"], value["project"], value


def provenance(config, evidence, *, retired=False):
    inventory = read(checked(evidence / "source-inventory.json", config["source_inventory_sha256"]))
    extras = evidence / "probe-inventory.json"
    if extras.exists():
        if ALLOCATION is not None:
            checked(extras, ALLOCATION["probe_inventory_sha256"])
        inventory.update(read(extras))
    verify_source(Path(config["runtime_source"]), inventory, retired=retired)
    ledger = read(checked(evidence / "migration-inventory.json", config["migration_inventory_sha256"]))
    epoch = read(checked(evidence / "binding.json", config["binding_sha256"]))
    build = read(checked(evidence / "importer-build.json", config["importer_build_sha256"]))
    if (build["commit"] != epoch["commit"] or build["binary_sha256"] != config["importer_sha256"]
            or build["source_inventory_sha256"] != config["source_inventory_sha256"]
            or build["migration_inventory_sha256"] != config["migration_inventory_sha256"]):
        raise RuntimeError("actual_importer_build_binding_mismatch")
    if migration_inventory(Path(config["runtime_source"]), epoch["last_migration"]) != ledger:
        raise RuntimeError("raw_migration_bytes_changed")
    return ledger


def ledger_check(config, evidence):
    expected = provenance(config, evidence)
    actual = json.loads(sql("SELECT coalesce(json_agg(json_build_object('name',name,"
                            "'sha256',checksum) ORDER BY name),'[]') FROM public.zns_schema_migrations;\n"))
    if actual != expected:
        raise RuntimeError("exact_raw_migration_ledger_required")


def quote(value):
    return '"' + value.replace('"', '""') + '"'


def snapshot(expected, check_counts=True):
    guard()
    tables = json.loads(sql("SELECT coalesce(json_agg(json_build_object('schema',n.nspname,"
                              "'name',c.relname,'kind',c.relkind) ORDER BY n.nspname,c.relname),'[]') "
                              "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                              "WHERE n.nspname IN('core','bot','interaction','public') "
                              "AND c.relkind IN('r','p','S');\n"))
    result = {}
    for table in tables:
        name = table["schema"] + "." + table["name"]
        identifier = quote(table["schema"]) + "." + quote(table["name"])
        if table["kind"] == "S":
            query = "SELECT json_build_object('last_value',last_value,'is_called',is_called) FROM " + identifier + ";\n"
        else:
            query = ("SELECT json_build_object('count',count(*),'sha256',"
                     "encode(sha256(convert_to(coalesce(string_agg(row_json::text,E'\\n' "
                     "ORDER BY row_json::text),''),'UTF8')),'hex')) FROM "
                     "(SELECT to_jsonb(t) AS row_json FROM " + identifier + " t) AS fingerprint_rows;\n")
        result[name] = json.loads(sql(query))
    for name in REQUIRED:
        if name not in result or result[name]["count"] < 1:
            raise RuntimeError("representative_imported_state_missing")
    if check_counts:
        for name, count in expected["counts"].items():
            if result.get(name, {}).get("count") != count:
                raise RuntimeError("expected_persistent_count_mismatch")
    # Original/owner-bound import references must genuinely link to runtime rows.
    linked = json.loads(sql("SELECT json_build_object('history',EXISTS(SELECT 1 FROM "
                            "core.legacy_message_references r JOIN core.conversation_events e ON e.id=r.event_id "
                            "JOIN core.conversation_message_bodies b ON b.event_id=e.id "
                            "WHERE r.owner=e.owner AND e.origin='original' AND NOT r.tombstoned),"
                            "'massage',EXISTS(SELECT 1 FROM core.legacy_massage_import_references r "
                            "JOIN core.legacy_massage_drafts d USING(source_key) WHERE r.owner=d.owner));\n"))
    if linked != {"history": True, "massage": True}:
        raise RuntimeError("imported_runtime_linkage_missing")
    return result


def archive_binding(config):
    evidence = Path(config["runtime_source"]).resolve(strict=True).parent
    prepared = read(checked(evidence / "prepared-import-bindings.json", config["prepared_import_bindings_sha256"]))
    source = Path(config["source_export"]).resolve(strict=True)
    manifest = json.loads((source / "manifest.json").read_text(encoding="utf-8"))
    if manifest["bot_id"] != 999 or not manifest["snapshot_id"].startswith("synthetic-e-"):
        raise RuntimeError("synthetic_selected_bot_export_required")
    included = {row["domain"] for row in manifest["coverage"] if row["status"] == "included"}
    if not {"users", "events", "orders", "passes", "massage", "messages", "knowledge", "schedule"} <= included:
        raise RuntimeError("cross_domain_source_coverage_missing")
    binding = {"source": tree(source), "stages": {}, "inputs": {}, "resources": {}}
    if binding["source"] != prepared["source"]:
        raise RuntimeError("prepared_source_export_changed")
    for domain in DOMAINS:
        entry = config["imports"][domain]
        binding["stages"][domain] = tree(entry["stage"])
        if binding["stages"][domain] != prepared["stage"]:
            raise RuntimeError("prepared_stage_changed")
        stage_manifest = Path(entry["stage"]) / "snapshot" / "manifest.json"
        if digest(stage_manifest) != digest(source / "manifest.json"):
            raise RuntimeError("all_domains_require_same_export")
        for key in ("plan", "resolutions"):
            actual = digest(entry[key])
            if actual != entry[key + "_sha256"]:
                raise RuntimeError("reviewed_import_input_hash_mismatch")
            binding["inputs"][domain + "." + key] = actual
    for name in ("knowledge.yaml", "lineup.csv"):
        entry = config["resources"][name]
        resource_domain = "knowledge" if name == "knowledge.yaml" else "schedule"
        declared = [row for row in manifest["files"] if row["path"] == entry["source_path"]]
        if (len(declared) != 1 or declared[0]["kind"] != "resource"
                or declared[0]["source"] != resource_domain or declared[0]["sha256"] != entry["sha256"]):
            raise RuntimeError("manifest_resource_declaration_missing")
        archived = (source / entry["source_path"]).resolve(strict=True)
        if not archived.is_relative_to(source) or digest(archived) != entry["sha256"]:
            raise RuntimeError("source_resource_binding_mismatch")
        if digest(entry["permanent_copy"]) != entry["sha256"]:
            raise RuntimeError("permanent_runtime_resource_mismatch")
        binding["resources"][name] = entry["sha256"]
    return binding


def mounted_resources(config):
    image = ALLOCATION["cli_image"]
    if not re.fullmatch(r"[^\s]+@sha256:[0-9a-f]{64}", image):
        raise RuntimeError("reviewed_digest_addressed_cli_image_required")
    output = command(["docker", "--context", "desktop-linux", "run", "--rm",
                      "--pull=never", "--network=none", "--read-only", "--user", "0:0",
                      "--cap-drop=ALL", "--entrypoint", "sha256sum", "--mount",
                      "type=volume,source=" + ALLOCATION["config_volume"] + ",target=/config,readonly",
                      image, "/config/knowledge.yaml", "/config/lineup.csv"])
    actual = {Path(row.split()[1]).name: row.split()[0] for row in output.splitlines()}
    required = {name: entry["sha256"] for name, entry in config["resources"].items()}
    if actual != required:
        raise RuntimeError("mounted_permanent_resource_bytes_changed")


def import_all(config, evidence):
    guard()
    ledger_check(config, evidence)
    if sql("SELECT to_regnamespace('migrate_import') IS NULL AND "
           "NOT EXISTS(SELECT 1 FROM core.users);\n").strip() != "t":
        raise RuntimeError("empty_new_import_database_required")
    binding = archive_binding(config)
    save(evidence / "archive-binding.json", binding)
    executable = Path(config["importer_binary"]).resolve(strict=True)
    if digest(executable) != config["importer_sha256"]:
        raise RuntimeError("importer_binary_hash_mismatch")
    for domain in DOMAINS:
        entry = config["imports"][domain]
        args = [str(executable), "apply", domain, "--stage", entry["stage"],
                "--plan", entry["plan"], "--resolutions", entry["resolutions"]]
        for phase in ("first", "replay"):
            response = json.loads(command(args))
            summary = response["apply_" + domain]
            if not summary["reconciled"]:
                raise RuntimeError("import_reconciliation_incomplete")
            if phase == "replay" and summary.get("applied", 0) != 0:
                raise RuntimeError("import_replay_added_rows")
            save(evidence / (domain + "-" + phase + ".json"), response)
    for domain in ("orders", "passes", "food", "massage", "messages"):
        entry = config["imports"][domain]
        response = json.loads(command([str(executable), "reconcile", domain,
                                       "--stage", entry["stage"], "--plan", entry["plan"],
                                       "--resolutions", entry["resolutions"]]))
        if not response["apply_" + domain]["reconciled"]:
            raise RuntimeError("final_import_reconciliation_incomplete")
        save(evidence / (domain + "-reconcile.json"), response)
    if archive_binding(config) != binding:
        raise RuntimeError("source_export_or_import_inputs_changed")
    mounted_resources(config)
    save(evidence / "before-removal.json", snapshot(config))


def remove_receipts(config, evidence):
    provenance(config, evidence)
    baseline = json.loads((evidence / "before-removal.json").read_text())
    if snapshot(config) != baseline:
        raise RuntimeError("permanent_state_changed_before_removal")
    binding = json.loads((evidence / "archive-binding.json").read_text())
    if archive_binding(config) != binding:
        raise RuntimeError("archive_binding_changed")
    mounted_resources(config)
    inventory = json.loads(sql("SELECT coalesce(json_agg(c.relname ORDER BY c.relname),'[]') "
                               "FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                               "WHERE n.nspname='migrate_import' AND c.relkind='r';\n"))
    if sorted(inventory) != sorted(RECEIPTS):
        raise RuntimeError("unexpected_temporary_schema_objects")
    receipt_rows = {}
    for name in RECEIPTS:
        receipt_rows[name] = json.loads(sql("SELECT coalesce(json_agg(to_jsonb(t)),'[]') FROM "
                                           "migrate_import." + quote(name) + " t;\n"))
    save(evidence / "temporary-receipts.json", receipt_rows)
    # No CASCADE: any unclassified dependency aborts the complete transaction.
    sql("BEGIN;\n" + "\n".join("DROP TABLE migrate_import." + quote(name) + " RESTRICT;"
                                for name in RECEIPTS) + "\nDROP SCHEMA migrate_import RESTRICT;\nCOMMIT;\n")
    if snapshot(config) != baseline:
        raise RuntimeError("removal_changed_permanent_state")
    save(evidence / "after-removal.json", snapshot(config))


def archive_importer(config, evidence):
    guard()
    provenance(config, evidence)
    if snapshot(config) != read(evidence / "after-removal.json"):
        raise RuntimeError("permanent_state_changed_before_archive")
    root = Path(config["runtime_source"]).resolve(strict=True)
    marker = json.loads((root / ".e-removal-owned.json").read_text())
    if marker != {"scope": SCOPE, "database": DATABASE}:
        raise RuntimeError("isolated_source_copy_ownership_required")
    if root == Path(config["repository_root"]).resolve() or not root.is_relative_to(evidence):
        raise RuntimeError("source_copy_must_be_inside_own_evidence")
    package = root / "tools" / "migrate"
    binary = Path(config["importer_binary"]).resolve(strict=True)
    if not binary.is_relative_to(root):
        raise RuntimeError("importer_binary_must_be_in_owned_source_copy")
    if binary.is_relative_to(package):
        raise RuntimeError("binary_must_be_separate_from_package")
    saved = evidence / "retired-importer"
    saved.mkdir()
    save(saved / "source-hashes.json", tree(package))
    shutil.move(str(package), str(saved / "migrate"))
    shutil.move(str(binary), str(saved / ("zns-migrate-retired" + binary.suffix)))
    if package.exists() or binary.exists():
        raise RuntimeError("importer_still_available")
    # Runtime dependency proof/build follows using a fresh cache, no importer DSN.
    save(evidence / "importer-unavailable.json", {"package_absent": True, "binary_absent": True})


def build_runtime(config, evidence, probes_path, probes_sha, *, retired):
    if retired and ALLOCATION is None:
        raise RuntimeError("reviewed_allocation_required")
    if retired and probes_sha != ALLOCATION["probes_sha256"]:
        raise RuntimeError("allocated_probe_binding_required")
    root = Path(config["runtime_source"]).resolve(strict=True)
    if root == Path(config["repository_root"]).resolve() or not root.is_relative_to(evidence):
        raise RuntimeError("isolated_build_source_required")
    provenance(config, evidence, retired=retired)
    if retired and ((root / "tools/migrate").exists() or Path(config["importer_binary"]).exists()
                    or not (evidence / "importer-unavailable.json").is_file()):
        raise RuntimeError("importer_retirement_required")
    probes = read(checked(probes_path, probes_sha))
    if set(probes) != {"removal", "coverage"}:
        raise RuntimeError("both_bound_probes_required")
    added = {}
    for name, entry in probes.items():
        source = checked(entry["source"], entry["sha256"])
        checked(entry["projection"], entry["projection_sha256"])
        relative = "platform/cmd/e-" + name + "-probe/main.go"
        target = safe_path(root, relative)
        if target.exists():
            checked(target, entry["sha256"])
        else:
            target.parent.mkdir(parents=True)
            target.write_bytes(source.read_bytes())
        added[relative] = entry["sha256"]
    inventory_file = evidence / "probe-inventory.json"
    if inventory_file.exists():
        if read(inventory_file) != added:
            raise RuntimeError("probe_sources_changed")
    else:
        save(inventory_file, added)
    phase = "retired" if retired else "offline"
    env = clean_build_env(evidence / (phase + "-build-cache"))
    record = {"importer_absent": retired, "probes_sha256": probes_sha, "programs": {}}
    for name, target in (("zns", "./cmd/zns"), ("removal", "./cmd/e-removal-probe"),
                         ("coverage", "./cmd/e-coverage-probe")):
        deps = command(["go", "list", "-deps", target], cwd=root / "platform", env=env)
        if "github.com/complynx/zns-chatbot/tools/migrate" in deps:
            raise RuntimeError("runtime_importer_dependency_rejected")
        binary = root / "dist" / (phase + "-" + name + (".exe" if os.name == "nt" else ""))
        argv = ["go", "build", "-mod=readonly", "-o", str(binary), target]
        command(argv, cwd=root / "platform", env=env)
        record["programs"][name] = {"command": argv, "exit": 0, "sha256": digest(binary),
                                      "dependencies": deps}
    provenance(config, evidence, retired=retired)
    save(evidence / (phase + "-runtime-build.json"), record)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("action", choices=("import", "remove-receipts", "archive-importer", "capture", "compare",
                                         "compile-probes", "build-runtime"))
    parser.add_argument("--inputs", required=True)
    parser.add_argument("--inputs-sha256", required=True)
    parser.add_argument("--evidence", required=True)
    parser.add_argument("--allocation")
    parser.add_argument("--allocation-sha256")
    parser.add_argument("--probes")
    parser.add_argument("--probes-sha256")
    parser.add_argument("--checkpoint")
    args = parser.parse_args()
    config = read(checked(args.inputs, args.inputs_sha256))
    evidence = Path(args.evidence).resolve(strict=True)
    if Path(args.inputs).resolve(strict=True) != evidence / "inputs.json":
        raise RuntimeError("evidence_bound_inputs_required")
    if args.action != "compile-probes":
        if not args.allocation or not args.allocation_sha256:
            raise RuntimeError("reviewed_allocation_required")
        allocate(args.allocation, args.allocation_sha256, config, evidence)
    if args.action in ("compile-probes", "build-runtime"):
        if not args.probes or not args.probes_sha256:
            raise RuntimeError("reviewed_probe_inputs_required")
        build_runtime(config, evidence, args.probes, args.probes_sha256, retired=args.action == "build-runtime")
        print(json.dumps({"action": args.action, "verified": True}))
        return
    if args.action == "import":
        import_all(config, evidence)
    elif args.action == "remove-receipts":
        remove_receipts(config, evidence)
    elif args.action == "archive-importer":
        archive_importer(config, evidence)
    else:
        if not args.checkpoint or Path(args.checkpoint).name != args.checkpoint:
            raise RuntimeError("simple_checkpoint_filename_required")
        mounted_resources(config)
        current = snapshot(config, check_counts=False)
        if args.action == "capture":
            save(evidence / args.checkpoint, current)
        elif json.loads((evidence / args.checkpoint).read_text()) != current:
            raise RuntimeError("permanent_state_drift")
    print(json.dumps({"action": args.action, "verified": True}))


if __name__ == "__main__":
    main()
