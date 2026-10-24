"""Whole local app-scenario DB switch; original DB/config retained for restore.

Explicit root dispatch only. No user-row purge, IAM mutation or automatic recovery.
"""
import argparse
import copy
import hashlib
import json
import pathlib
import re
import subprocess
import time
import ssl
import urllib.parse
import urllib.request
import operator_cli as qa


def run(argv, **kwargs):
    return subprocess.run(argv, check=True, timeout=180, stderr=subprocess.PIPE, **kwargs)


def inspect(cid):
    value = json.loads(subprocess.check_output(["docker", "inspect", cid], text=True, timeout=20))[0]
    labels = value["Config"]["Labels"]
    qa.need(value["Id"] == cid and labels.get("com.docker.compose.project") == qa.PROJECT and labels.get("zinusya.qa.owner") == qa.OWNER, "Wrong local owned container")
    return value


def identifier(value):
    qa.need(re.fullmatch(r"[a-z][a-z0-9_]{0,62}", value) is not None, "Invalid bounded SQL identifier")
    return '"' + value + '"'


def dsn_database(dsn, database):
    parsed = urllib.parse.urlsplit(dsn)
    qa.need(parsed.scheme == "postgres" and parsed.hostname == "postgres" and parsed.username, "Expected private local PG DSN")
    identifier(database)
    return urllib.parse.urlunsplit(parsed._replace(path="/" + database))


def sql(cfg, query, database="postgres"):
    return subprocess.check_output(["docker", "exec", cfg["postgres_id"], "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", cfg["postgres_admin"], "-d", database, "-At", "-c", query], stderr=subprocess.PIPE, text=True, timeout=30).strip()


def compose(cfg, *args, overlay=None):
    command = ["docker", "compose", "-p", qa.PROJECT, "-f", cfg["compose_custody"]["path"]]
    if overlay:
        command += ["-f", str(overlay)]
    return run(command + list(args), stdout=subprocess.PIPE)


def fences(cfg):
    qa.need({f["name"] for f in cfg["fences"]} == {"receiver", "provider"}, "Both ingress and paid-provider fences required")
    for fence in cfg["fences"]:
        qa.need(qa.sha(fence["path"]) == fence["sha256"], "Manual runtime fence changed")
        value = qa.load(fence["path"])
        qa.need(fence["expected_fields"], "Explicit held fields required")
        for key, expected in fence["expected_fields"].items():
            qa.need(qa.pointer(value, key) == expected, "Manual runtime not held")


def admission(cfg):
    qa.need((cfg["project"], cfg["owner"], cfg["bot_id"]) == (qa.PROJECT, qa.OWNER, qa.BOT), "Wrong stand")
    qa.need(cfg["issuer"] == "https://iam.zns.localhost:28443" and cfg["organization_id"], "Wrong scope")
    for key in ("metadata", "app_config", "migrator_config", "compose_custody"):
        qa.need(qa.sha(cfg[key]["path"]) == cfg[key]["sha256"], "Changed custody: " + key)
    meta = qa.load(cfg["metadata"]["path"])
    qa.need(meta["project"] == qa.PROJECT and meta["owner"] == qa.OWNER, "Wrong metadata")
    rows = {row["service"]: row for row in meta["services"]}
    qa.need(rows["postgres"]["id"] == cfg["postgres_id"] and rows["app"]["id"] == cfg["app_id"], "Stale selected containers")
    for name, row in rows.items():
        actual = inspect(row["id"])
        qa.need(actual["Image"] == row["image"] and actual["State"]["Running"] and not actual["State"].get("OOMKilled"), "Container changed: " + name)
    fences(cfg)
    app = qa.load(cfg["app_config"]["path"])
    migration = qa.load(cfg["migrator_config"]["path"])
    for value in (app, migration):
        qa.need(value["identity"]["issuer"] == cfg["issuer"] and value["identity"]["organization_id"] == cfg["organization_id"], "Config deployment mismatch")
        qa.need(value["telegram"]["token"].split(":", 1)[0] == str(qa.BOT), "Wrong bot config")
    db = urllib.parse.urlsplit(app["database_url"]).path[1:]
    qa.need(db == cfg["original_database"] and urllib.parse.urlsplit(migration["database_url"]).path[1:] == db, "Wrong original app database")
    binding = json.loads(sql(cfg, "SELECT json_build_object('bot_id',bot_id,'organization_id',organization_id,'issuer',issuer) FROM shuttle_deployment;", db))
    qa.need(binding == {"bot_id": qa.BOT, "organization_id": cfg["organization_id"], "issuer": cfg["issuer"]}, "Original deployment binding mismatch")
    idle(cfg, db)
    return app, migration, rows


def idle(cfg, db):
    state = json.loads(sql(cfg, "SELECT json_build_object('tasks',(SELECT count(*) FROM shuttle_tasks WHERE state NOT IN ('completed','closed','failed','cancelled')),'outbox',(SELECT count(*) FROM shuttle_outbox WHERE state<>'delivered'));", db))
    qa.need(state == {"tasks": 0, "outbox": 0}, "Pending/unknown app work blocks scenario switch")
    state = json.loads(sql(cfg, "SELECT json_build_object('runs',(SELECT count(*) FROM runs WHERE state NOT IN ('completed','failed','cancelled')),'steps',(SELECT count(*) FROM steps WHERE state IN ('pending','unknown')));", cfg["loom_database"]))
    qa.need(state == {"runs": 0, "steps": 0}, "Pending/unknown manual Loom work blocks scenario switch")


def protected_directory(directory):
    # Shared tracked CLI performs exact current-owner-only DACL/mode check before its
    # own journal. Scenario setup must use an independently protected existing folder.
    import os
    directory = pathlib.Path(directory)
    qa.need(directory.is_dir() and not any(directory.iterdir()) and not directory.is_symlink(), "Fresh protected scenario directory required")
    if os.name == "nt":
        script = "$a=Get-Acl -LiteralPath $env:SHUTTLE_QA_AUDIT;$r=@($a.Access);$s=[System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value;if(-not $a.AreAccessRulesProtected -or $r.Count -ne 1 -or $r[0].IsInherited -or $r[0].IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value -ne $s -or $r[0].AccessControlType -ne 'Allow' -or $r[0].FileSystemRights -ne 'FullControl' -or $r[0].InheritanceFlags -ne ([System.Security.AccessControl.InheritanceFlags]::ObjectInherit -bor [System.Security.AccessControl.InheritanceFlags]::ContainerInherit) -or $r[0].PropagationFlags -ne [System.Security.AccessControl.PropagationFlags]::None){throw 'Private DACL required'}"
        run(["pwsh", "-NoProfile", "-NonInteractive", "-Command", script], env={**os.environ, "SHUTTLE_QA_AUDIT": str(directory)}, stdout=subprocess.PIPE)
    else:
        qa.need(directory.stat().st_mode & 0o077 == 0, "Private mode required")
    return directory


def snapshot(cfg, directory):
    archive = directory / "original-app.dump"
    with archive.open("xb") as stream:
        run(["docker", "exec", cfg["postgres_id"], "pg_dump", "-U", cfg["postgres_admin"], "-Fc", "-d", cfg["original_database"]], stdout=stream)
    # Bound the whole decode; private file sinks avoid pipe deadlock and retain errors.
    decoded = directory / "original-app.decoded.sql"
    with archive.open("rb") as stream, decoded.open("xb") as output, (directory / "decode.stderr.private.log").open("xb") as errors:
        subprocess.run(["docker", "exec", "-i", cfg["postgres_id"], "pg_restore", "--file=-"], stdin=stream, stdout=output, stderr=errors, timeout=180, check=True)
    length = decoded.stat().st_size
    qa.need(length > 0, "Archive full decode empty")
    digest = qa.sha(decoded)
    decoded.unlink()  # Only this helper's successful temporary decode; archive is retained.
    return {"path": str(archive), "sha256": qa.sha(archive), "fully_decoded": True, "decoded_sha256": digest, "decoded_bytes": length}


def create(cfg, name, directory):
    qa.need(re.fullmatch("qa_[a-z0-9_]{1,36}", name) is not None and name != cfg["original_database"], "Fresh qa_ database name required")
    app, migration, rows = admission(cfg)
    qa.need(sql(cfg, "SELECT count(*) FROM pg_database WHERE datname='" + name + "';") == "0", "Scenario DB already exists")
    directory = protected_directory(directory)
    qa.write_once(directory / "intent.json", {"action": "whole_app_scenario_create", "config": cfg, "database": name, "scope": "whole app database, IAM unchanged"})
    run(["docker", "stop", "--time", "30", cfg["app_id"]], stdout=subprocess.PIPE)
    qa.need(not inspect(cfg["app_id"])["State"]["Running"], "App writer not stopped")
    idle(cfg, cfg["original_database"])
    backup = snapshot(cfg, directory)
    qa.write_once(directory / "backup.json", backup)
    runtime_role = urllib.parse.unquote(urllib.parse.urlsplit(app["database_url"]).username)
    migrator_role = urllib.parse.unquote(urllib.parse.urlsplit(migration["database_url"]).username)
    sql(cfg, "CREATE DATABASE " + identifier(name) + " OWNER " + identifier(migrator_role) + ";")
    sql(cfg, "REVOKE ALL ON DATABASE " + identifier(name) + " FROM PUBLIC; GRANT CONNECT,TEMPORARY ON DATABASE " + identifier(name) + " TO " + identifier(runtime_role) + ";")
    sql(cfg, "REVOKE CREATE ON SCHEMA public FROM PUBLIC; ALTER DEFAULT PRIVILEGES FOR ROLE " + identifier(migrator_role) + " IN SCHEMA public GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO " + identifier(runtime_role) + "; ALTER DEFAULT PRIVILEGES FOR ROLE " + identifier(migrator_role) + " IN SCHEMA public GRANT USAGE,SELECT,UPDATE ON SEQUENCES TO " + identifier(runtime_role) + ";", name)
    new_app, new_migration = copy.deepcopy(app), copy.deepcopy(migration)
    new_app["database_url"] = dsn_database(app["database_url"], name)
    new_migration["database_url"] = dsn_database(migration["database_url"], name)
    qa.write_once(directory / "app.json", new_app)
    qa.write_once(directory / "migrator.json", new_migration)
    run(["docker", "run", "--rm", "--network", cfg["backend_network"], "--label", "zinusya.qa.owner=" + qa.OWNER,
         "--label", "com.docker.compose.project=" + qa.PROJECT, "--mount", "type=bind,src=" + str((directory / "migrator.json").resolve()) + ",dst=/config/migrator.json,readonly",
         "--mount", "type=bind,src=" + cfg["ca_file"] + ",dst=/tls/ca-bundle.pem,readonly", rows["app"]["image"], "-config", "/config/migrator.json", "-migrate"], stdout=subprocess.PIPE)
    sql(cfg, "GRANT USAGE ON SCHEMA public TO " + identifier(runtime_role) + "; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA public TO " + identifier(runtime_role) + "; GRANT USAGE,SELECT,UPDATE ON ALL SEQUENCES IN SCHEMA public TO " + identifier(runtime_role) + ";", name)
    overlay = directory / "compose.json"
    qa.write_once(overlay, {"services": {"app": {"volumes": [{"type": "bind", "source": str((directory / "app.json").resolve()), "target": "/config/app.json", "read_only": True}]}}})
    fences(cfg)
    compose(cfg, "up", "-d", "--no-deps", "--force-recreate", "app", overlay=overlay)
    new_id = compose_id(cfg)
    actual = inspect(new_id)
    qa.need(actual["Image"] == rows["app"]["image"] and actual["State"]["Running"], "New scenario app not running")
    for service, row in rows.items():
        if service != "app":
            actual = inspect(row["id"])
            qa.need(actual["Image"] == row["image"] and actual["State"]["StartedAt"] == row["started_at"], "Other service changed")
    ready(cfg)
    binding = json.loads(sql(cfg, "SELECT json_build_object('bot_id',bot_id,'organization_id',organization_id,'issuer',issuer) FROM shuttle_deployment;", name))
    qa.need(binding == {"bot_id": qa.BOT, "organization_id": cfg["organization_id"], "issuer": cfg["issuer"]}, "Fresh scenario deployment binding mismatch")
    counts = json.loads(sql(cfg, "SELECT json_build_object('users',(SELECT count(*) FROM shuttle_users),'tasks',(SELECT count(*) FROM shuttle_tasks),'history',(SELECT count(*) FROM shuttle_legacy_history_records),'outbox',(SELECT count(*) FROM shuttle_outbox));", name))
    qa.need(all(value == 0 for value in counts.values()), "Scenario not empty; keep ingress held")
    qa.write_once(directory / "receipt.json", {"phase": "fresh_app_scenario_ready_held", "project": qa.PROJECT, "owner": qa.OWNER, "new_app_id": new_id, "original_database_retained": cfg["original_database"], "scenario_database": name, "backup": backup, "config": cfg, "binding": binding, "empty_counts": counts, "IAM_unchanged": True, "acceptance_pass": False, "next": "root selects actual manual registration; receiver/provider remain held until explicit dispatch"})
    return {"app_ready": True, "fresh_whole_scenario": True, "IAM_unchanged": True}


def compose_id(cfg):
    cid = subprocess.check_output(["docker", "compose", "-p", qa.PROJECT, "-f", cfg["compose_custody"]["path"], "ps", "-q", "app"], text=True, timeout=20).strip()
    qa.need(cid and "\n" not in cid, "Exactly one app container required")
    return json.loads(subprocess.check_output(["docker", "inspect", cid], text=True, timeout=20))[0]["Id"]


def ready(cfg):
    parsed = urllib.parse.urlsplit(cfg["ready_url"])
    qa.need(parsed.scheme == "https" and parsed.hostname in ("iam.zns.localhost", "zns.localhost", "localhost", "127.0.0.1") and not parsed.username and not parsed.password and not parsed.query and not parsed.fragment and parsed.path == "/readyz", "Only local verified readiness endpoint")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), qa.NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=cfg["ca_file"])))
    deadline = time.monotonic() + 45
    while time.monotonic() < deadline:
        try:
            with opener.open(cfg["ready_url"], timeout=3) as response:
                if response.status == 200 and response.geturl() == cfg["ready_url"]:
                    return
        except (OSError, urllib.error.URLError):
            pass
        time.sleep(0.5)
    raise ValueError("App readiness not established; preserve intent, do not repeat switch")


def restore(cfg, receipt, directory):
    qa.need(receipt["phase"] == "fresh_app_scenario_ready_held" and receipt["project"] == qa.PROJECT and receipt["owner"] == qa.OWNER, "Exact successful scenario receipt required")
    old = receipt["config"]
    qa.need({k: v for k, v in cfg.items() if k != "fences"} == {k: v for k, v in old.items() if k != "fences"}, "Restore config drift except newly reviewed held fences")
    fences(cfg)
    actual = inspect(receipt["new_app_id"])
    original_meta = qa.load(cfg["metadata"]["path"])
    rows = {r["service"]: r for r in original_meta["services"]}
    qa.need(actual["Image"] == rows["app"]["image"] and actual["State"]["Running"], "Scenario app changed")
    for key in ("metadata", "app_config", "compose_custody"):
        qa.need(qa.sha(cfg[key]["path"]) == cfg[key]["sha256"], "Original custody drift")
    current_cfg = {**cfg, "original_database": receipt["scenario_database"]}
    state = json.loads(sql(cfg, "SELECT json_build_object('tasks',(SELECT count(*) FROM shuttle_tasks WHERE state NOT IN ('completed','closed','failed','cancelled')),'outbox',(SELECT count(*) FROM shuttle_outbox WHERE state<>'delivered'));", receipt["scenario_database"]))
    qa.need(state == {"tasks": 0, "outbox": 0}, "Scenario active/unknown work blocks restore")
    loom = json.loads(sql(cfg, "SELECT json_build_object('runs',(SELECT count(*) FROM runs WHERE state NOT IN ('completed','failed','cancelled')),'steps',(SELECT count(*) FROM steps WHERE state IN ('pending','unknown')));", cfg["loom_database"]))
    qa.need(loom == {"runs": 0, "steps": 0}, "Loom unresolved work blocks restore")
    for name, row in rows.items():
        if name != "app":
            qa.need(inspect(row["id"])["Image"] == row["image"], "Other service changed")
    directory = protected_directory(directory)
    qa.write_once(directory / "intent.json", {"action": "restore_original_whole_app_scenario", "original_receipt": receipt})
    run(["docker", "stop", "--time", "30", receipt["new_app_id"]], stdout=subprocess.PIPE)
    qa.need(not inspect(receipt["new_app_id"])["State"]["Running"], "Scenario app writer not stopped")
    idle(cfg, receipt["scenario_database"])
    backup = snapshot(current_cfg, directory)
    qa.write_once(directory / "backup.json", backup)
    fences(cfg)
    compose(cfg, "up", "-d", "--no-deps", "--force-recreate", "app")
    restored = inspect(compose_id(cfg))
    qa.need(restored["Image"] == rows["app"]["image"], "Restored image mismatch")
    ready(cfg)
    qa.write_once(directory / "receipt.json", {"phase": "original_app_scenario_restored_held", "app_id": restored["Id"], "scenario_database_retained": receipt["scenario_database"], "original_database": cfg["original_database"], "scenario_backup": backup, "IAM_unchanged": True})
    return {"original_app_scenario_restored": True, "scenario_database_retained": True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--name")
    parser.add_argument("--restore-receipt")
    parser.add_argument("--restore-receipt-sha256")
    parser.add_argument("--private-dir")
    parser.add_argument("--apply-config-sha256")
    args = parser.parse_args()
    cfg = qa.load(args.config)
    if args.apply_config_sha256 is None:
        print(json.dumps({"plan_only": True, "scope": "whole app scenario; IAM retained", "database": args.name, "config_sha256": qa.sha(args.config), "steps": ["hold ingress/provider", "read idle state and exact ownership", "stop only app", "pg_dump-Fc + full pg_restore decode", "create fresh DB using existing separate migrator/runtime", "maintained -migrate", "app-only compose overlay recreate", "root readiness and empty-scope proof"], "restore": "separate reviewed original compose app-only switch after snapshot/drain; never drop original DB"}, indent=2))
        return
    qa.need(qa.sha(args.config) == args.apply_config_sha256 and args.private_dir, "Explicit reviewed config hash/private directory required")
    if args.restore_receipt:
        qa.need(qa.sha(args.restore_receipt) == args.restore_receipt_sha256, "Reviewed restore receipt hash required")
        result = restore(cfg, qa.load(args.restore_receipt), args.private_dir)
    else:
        qa.need(args.name, "Explicit new scenario database name required")
        result = create(cfg, args.name, args.private_dir)
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"stopped": True, "error_type": type(error).__name__, "no_automatic_retry": True, "next": "Preserve intent and inspect actual phase; no automatic restart, delete or rollback"}))
        raise SystemExit(1)
