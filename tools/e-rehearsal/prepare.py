"""Offline E preparation from exact Git archive bytes; no database or Docker calls."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import re
import subprocess
import zipfile

DOMAINS = ("users", "events", "orders", "passes", "food", "massage", "messages")
HEX = re.compile(r"[a-f0-9]{64}")
SCOPE = "qa.e-import-removal.20261001.synthetic-only"


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def read(path):
    return json.loads(Path(path).read_text(encoding="utf-8-sig"))


def save(path, value):
    with Path(path).open("x", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2, sort_keys=True)


def safe_path(root, name):
    relative = PurePosixPath(name)
    if relative.is_absolute() or ".." in relative.parts or "\\" in name or ":" in name:
        raise RuntimeError("relative_evidence_path_required")
    target = Path(root).joinpath(*relative.parts)
    for part in (target, *target.parents):
        if part.is_symlink() or (hasattr(part, "is_junction") and part.is_junction()):
            raise RuntimeError("linked_evidence_path_rejected")
    if not target.resolve().is_relative_to(Path(root).resolve()):
        raise RuntimeError("evidence_path_escape")
    return target


def tree(root):
    root = Path(root).resolve(strict=True)
    result = {}
    for item in sorted(root.rglob("*")):
        safe_path(root, item.relative_to(root).as_posix())
        if item.is_file():
            result[item.relative_to(root).as_posix()] = digest(item)
    return result


def checked(path, sha):
    if not HEX.fullmatch(sha) or digest(path) != sha:
        raise RuntimeError("reviewed_file_hash_mismatch")
    return Path(path).resolve(strict=True)


def command(argv, *, cwd=None, data=None, env=None, binary=False):
    result = subprocess.run(argv, cwd=cwd, input=data, env=env, text=not binary,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=600, check=False)
    if result.returncode:
        # Never include stderr, DSNs or imported private input in errors.
        raise RuntimeError("e_command_failed")
    return result.stdout


def clean_build_env(cache):
    env = os.environ.copy()
    for key in tuple(env):
        if key.startswith(("MIGRATE_", "E_RUNTIME_", "ZNS_", "PG")):
            del env[key]
    env.update(GOWORK="off", GOCACHE=str(cache), GOPROXY="off", GOTOOLCHAIN="local")
    return env


def binding(path, sha, *, frozen=False):
    checked(path, sha)
    value = read(path)
    if (not re.fullmatch(r"[a-f0-9]{40}", value.get("commit", ""))
            or not re.fullmatch(r"[0-9]{3}_[a-z0-9_]+\.sql", value.get("last_migration", ""))
            or not re.fullmatch(r"synthetic_qa_zns_[a-z0-9_]+", value.get("database", ""))
            or not re.fullmatch(r"synthetic-qa-zns-[a-z0-9-]+", value.get("project", ""))
            or value.get("marker") != SCOPE
            or value.get("status") not in ("offline", "frozen")
            or not value.get("owner")):
        raise RuntimeError("explicit_synthetic_epoch_binding_required")
    if frozen and (value["status"] != "frozen" or not value.get("reviewed_schema")
                   or int(value["last_migration"][:3]) < 90):
        raise RuntimeError("reviewed_final_epoch_allocation_required")
    return value


def archive_source(repository, epoch, destination):
    actual = command(["git", "rev-parse", epoch["commit"] + "^{commit}"], cwd=repository).strip()
    if actual != epoch["commit"]:
        raise RuntimeError("exact_git_commit_required")
    command(["git", "diff", "--exit-code", epoch["commit"], "--", "platform", "tools/migrate"],
            cwd=repository)
    if command(["git", "ls-files", "--others", "--exclude-standard", "--", "platform", "tools/migrate"],
               cwd=repository).strip():
        raise RuntimeError("untracked_runtime_source_rejected")
    tracked = {}
    listing = command(["git", "ls-tree", "-rz", epoch["commit"], "--", "platform", "tools/migrate"],
                      cwd=repository, binary=True)
    for row in listing.split(b"\0"):
        if not row:
            continue
        metadata, name = row.split(b"\t", 1)
        mode, kind, blob = metadata.split()
        if kind != b"blob" or mode not in (b"100644", b"100755"):
            raise RuntimeError("ordinary_git_source_files_required")
        tracked[name.decode("utf-8")] = blob.decode("ascii")
    raw = command(["git", "archive", "--format=zip", epoch["commit"], "platform", "tools/migrate"],
                  cwd=repository, binary=True)
    inventory = {}
    with zipfile.ZipFile(io.BytesIO(raw)) as archive:
        for entry in archive.infolist():
            target = safe_path(destination, entry.filename)
            if entry.is_dir():
                continue
            if entry.external_attr >> 16 & 0o170000 == 0o120000:
                raise RuntimeError("git_symlink_rejected")
            if entry.filename in inventory:
                raise RuntimeError("duplicate_git_archive_path")
            body = archive.read(entry)
            blob_body = b"blob " + str(len(body)).encode("ascii") + b"\0" + body
            blob_sha = hashlib.sha1(blob_body, usedforsecurity=False).hexdigest()
            if tracked.get(entry.filename) != blob_sha:
                raise RuntimeError("git_export_blob_bytes_changed")
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(body)
            inventory[entry.filename] = hashlib.sha256(body).hexdigest()
    if set(inventory) != set(tracked):
        raise RuntimeError("complete_tracked_source_export_required")
    verify_source(destination, inventory)
    return inventory


def verify_source(root, expected, *, retired=False):
    actual = {}
    for prefix in ("platform", "tools/migrate"):
        directory = safe_path(root, prefix)
        if directory.exists():
            actual.update({prefix + "/" + name: sha for name, sha in tree(directory).items()})
    wanted = {name: sha for name, sha in expected.items()
              if not (retired and name.startswith("tools/migrate/"))}
    if actual != wanted:
        raise RuntimeError("exact_runtime_source_inventory_required")


def migration_inventory(root, last):
    prefix = "platform/internal/store/migrations"
    rows = [{"name": item.name, "sha256": digest(item)}
            for item in sorted(safe_path(root, prefix).glob("*.sql"))]
    if not rows or rows[-1]["name"] != last:
        raise RuntimeError("migration_epoch_mismatch")
    return rows


def verify_package(package, inventory_path, inventory_sha):
    checked(inventory_path, inventory_sha)
    rows = read(inventory_path)
    names = set()
    for row in rows:
        if row["path"] in names:
            raise RuntimeError("duplicate_package_path")
        names.add(row["path"])
        checked(safe_path(package, row["path"]), row["sha256"])
    required = {"decisions.json", "expected-counts.json", "probe-spec.json", "source/manifest.json"}
    if not required <= names:
        raise RuntimeError("complete_reviewed_package_required")
    source = safe_path(package, "source")
    expected = {name.removeprefix("source/"): row["sha256"] for row in rows
                if (name := row["path"]).startswith("source/")}
    if tree(source) != expected:
        raise RuntimeError("exact_synthetic_source_inventory_required")
    manifest = read(source / "manifest.json")
    included = {row["domain"] for row in manifest["coverage"] if row["status"] == "included"}
    if (manifest["bot_id"] != 999 or not manifest["snapshot_id"].startswith("synthetic-e-")
            or len(included) != 12 or sum(row.get("records", 0) for row in manifest["files"]) != 23):
        raise RuntimeError("complete_synthetic_export_required")
    return source


def prepare(args):
    epoch = binding(args.binding, args.binding_sha256)
    repository = Path(args.repository).resolve(strict=True)
    package = Path(args.package).resolve(strict=True)
    source = verify_package(package, args.package_inventory, args.package_inventory_sha256)
    evidence = Path(args.evidence).absolute()
    safe_path(evidence.parent, evidence.name)
    if (not evidence.resolve().is_relative_to(repository / "qa.local")
            or evidence.resolve() == repository / "qa.local"):
        raise RuntimeError("new_repository_local_qa_evidence_required")
    evidence.mkdir()  # Exclusive creation; never overwrite earlier evidence.
    root = evidence / "runtime-source"
    root.mkdir()
    inventory = archive_source(repository, epoch, root)
    ledger = migration_inventory(root, epoch["last_migration"])
    save(root / ".e-removal-owned.json", {"scope": epoch["marker"], "database": epoch["database"]})
    save(evidence / "source-inventory.json", inventory)
    save(evidence / "migration-inventory.json", ledger)
    save(evidence / "binding.json", epoch)
    binary = root / "dist" / ("zns-migrate.exe" if os.name == "nt" else "zns-migrate")
    binary.parent.mkdir()
    env = clean_build_env(evidence / "build-cache")
    build = ["go", "build", "-mod=readonly", "-o", str(binary), "./cmd/zns-migrate"]
    command(build, cwd=root / "tools/migrate", env=env)
    verify_source(root, inventory)
    save(evidence / "importer-build.json", {
        "commit": epoch["commit"], "command": build, "exit": 0,
        "compiler": command(["go", "version"], env=env).strip(),
        "modules": command(["go", "list", "-m", "all"], cwd=root / "tools/migrate", env=env),
        "source_inventory_sha256": digest(evidence / "source-inventory.json"),
        "migration_inventory_sha256": digest(evidence / "migration-inventory.json"),
        "binary_sha256": digest(binary)})
    stage = evidence / "stage"
    for name in ("plans", "resolutions", "permanent-config"):
        (evidence / name).mkdir()
    def cli(arguments, log):
        output = command([str(binary), *arguments], env=env)
        value = json.loads(output)
        save(evidence / log, value)
    cli(["verify", "--snapshot", str(source)], "verify.json")
    cli(["stage", "--snapshot", str(source), "--out", str(stage)], "stage.json")
    decisions = read(package / "decisions.json")
    imports = {}
    for domain in DOMAINS:
        plan = evidence / "plans" / (domain + ".json")
        cli(["plan", domain, "--stage", str(stage), "--out", str(plan)], domain + "-plan-result.json")
        resolution = evidence / "resolutions" / (domain + ".json")
        decision = decisions[domain].copy()
        decision["plan_sha256"] = digest(plan)
        save(resolution, decision)
        imports[domain] = {"stage": str(stage), "plan": str(plan), "plan_sha256": digest(plan),
                           "resolutions": str(resolution), "resolutions_sha256": digest(resolution)}
    cli(["validate", "messages", "--stage", str(stage), "--plan", imports["messages"]["plan"],
         "--resolutions", imports["messages"]["resolutions"]], "messages-validation.json")
    resources = {}
    for name in ("knowledge.yaml", "lineup.csv"):
        copy = evidence / "permanent-config" / name
        copy.write_bytes((source / name).read_bytes())
        resources[name] = {"source_path": name, "permanent_copy": str(copy), "sha256": digest(copy)}
    counts = read(package / "expected-counts.json")
    counts["public.zns_schema_migrations"] = len(ledger)
    save(evidence / "prepared-import-bindings.json", {"source": tree(source), "stage": tree(stage)})
    save(evidence / "inputs.json", {
        "repository_root": str(repository), "runtime_source": str(root), "source_export": str(source),
        "importer_binary": str(binary), "importer_sha256": digest(binary), "imports": imports,
        "resources": resources, "counts": counts, "binding_sha256": digest(evidence / "binding.json"),
        "source_inventory_sha256": digest(evidence / "source-inventory.json"),
        "migration_inventory_sha256": digest(evidence / "migration-inventory.json"),
        "prepared_import_bindings_sha256": digest(evidence / "prepared-import-bindings.json"),
        "importer_build_sha256": digest(evidence / "importer-build.json"),
        "package_inventory_sha256": args.package_inventory_sha256})
    save(evidence / "materialization-status.json", {
        "commit": epoch["commit"], "last_migration": epoch["last_migration"],
        "actual_cli_preparation": True, "apply": False, "reconciliation": False,
        "input_review": "pending", "runtime_acceptance": "pending"})


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("repository", "binding", "binding-sha256", "package", "package-inventory",
                 "package-inventory-sha256", "evidence"):
        parser.add_argument("--" + name, required=True)
    prepare(parser.parse_args())


if __name__ == "__main__":
    main()
