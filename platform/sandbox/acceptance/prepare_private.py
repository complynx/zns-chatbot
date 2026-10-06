#!/usr/bin/env python3
"""Create fresh synthetic credentials in an approved empty private directory."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import stat


DATABASE = "synthetic_qa_zns_registration_fixture"


def prepare(directory: Path) -> None:
    # The operator admits the actual host directory and its ACL before dispatch.
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        if os.listdir(descriptor):
            raise ValueError("private directory must be empty")
        prepare_open_directory(descriptor)
    finally:
        os.close(descriptor)


def prepare_open_directory(directory_descriptor: int) -> None:
    passwords = {role: secrets.token_hex(32) for role in
                 ("postgres", "app", "meter", "inventory", "fake", "operator")}
    signing_key = secrets.token_hex(32)
    media_secret = secrets.token_hex(32)
    sticker_secret = secrets.token_hex(32)
    telegram_token = "8123:" + secrets.token_hex(32)

    def database_url(role: str) -> str:
        username = {"postgres": "postgres", "operator": "zns_registration_operator"}.get(role, "zns_" + role)
        return f"postgres://{username}:{passwords[role]}@postgres:5432/{DATABASE}?sslmode=disable"

    common = {"ZNS_AUTH__SIGNING_KEY": signing_key,
              "ZNS_TELEGRAM__TOKEN": telegram_token}
    environments = {
        "app": {**common, "ZNS_DATABASE__URL": database_url("app"),
                "ZNS_MEDIA__SECRET": media_secret,
                "ZNS_STICKER__WORKER__SECRET": sticker_secret},
        "owner": {**common, "ZNS_DATABASE__URL": database_url("app")},
        "fake": {**common, "ZNS_DATABASE__URL": database_url("fake")},
        "operator": {**common, "ZNS_DATABASE__URL": database_url("operator")},
        "media": {"DATABASE_URL": database_url("meter"),
                  "MEDIA_WORKER_SECRET": media_secret,
                  "OPENAI_API_KEY": "synthetic-acceptance-no-paid-provider",
                  "ZNS_CREDITS__ENFORCE": "false"},
        "sticker": {"STICKER_WORKER_SECRET": sticker_secret},
        "roles": {"PGHOST": "postgres", "PGPORT": "5432", "PGDATABASE": DATABASE,
                  "PGUSER": "zns_app", "PGPASSWORD": passwords["app"]},
        "inventory": {"PGHOST": "postgres", "PGPORT": "5432", "PGDATABASE": DATABASE,
                      "PGUSER": "zns_inventory", "PGPASSWORD": passwords["inventory"]},
    }
    files = {role + ".password": (password + "\n").encode("ascii")
             for role, password in passwords.items()}
    for name, values in environments.items():
        files[name + ".env"] = "".join(f"{key}={value}\n" for key, value in values.items()).encode("ascii")

    created = []
    try:
        for name, content in files.items():
            descriptor = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW,
                                 0o600, dir_fd=directory_descriptor)
            created.append(name)
            with os.fdopen(descriptor, "wb") as output:
                output.write(content)
                output.flush()
                os.fsync(output.fileno())
    except BaseException:
        for name in created:
            os.unlink(name, dir_fd=directory_descriptor)
        raise


def admit_native_copy(directory: Path, manifest: Path) -> None:
    """Admit unchanged copied bytes with root/private and app-only Linux access."""
    if os.getuid() != 0 or os.geteuid() != 0 or not {70, 10001}.issubset(os.getgroups()):
        raise ValueError("native preparation root with pinned PG and app groups required")
    manifest_descriptor = os.open(manifest, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        if not stat.S_ISREG(os.fstat(manifest_descriptor).st_mode):
            raise ValueError("regular native private manifest required")
        with os.fdopen(os.dup(manifest_descriptor), "rb") as stream:
            raw_manifest = stream.read(32769)
        if len(raw_manifest) > 32768:
            raise ValueError("bounded native private manifest required")
        expected = json.loads(raw_manifest)
    finally:
        os.close(manifest_descriptor)
    names = {"app.env", "owner.env", "fake.env", "operator.env", "media.env",
             "sticker.env", "roles.env", "inventory.env", "postgres.password",
             "app.password", "meter.password", "inventory.password",
             "fake.password", "operator.password"}
    if type(expected) is not dict or set(expected) != names:
        raise ValueError("exact native private file inventory required")
    descriptor = os.open(directory, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    try:
        if set(os.listdir(descriptor)) != names or os.fstat(descriptor).st_uid != 0:
            raise ValueError("owned native private directory required")
        for name in sorted(names):
            binding = expected[name]
            if (type(binding) is not dict or set(binding) != {"sha256", "bytes"}
                    or type(binding["bytes"]) is not int or not 0 < binding["bytes"] <= 1048576
                    or type(binding["sha256"]) is not str or len(binding["sha256"]) != 64):
                raise ValueError("typed bounded native private pin required")
            file = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=descriptor)
            try:
                observed = os.fstat(file)
                if (not stat.S_ISREG(observed.st_mode) or observed.st_uid != 0
                        or observed.st_size != binding["bytes"]):
                    raise ValueError("owned exact native private file required")
                with os.fdopen(os.dup(file), "rb") as stream:
                    raw = stream.read(1048577)
                if len(raw) != binding["bytes"] or hashlib.sha256(raw).hexdigest() != binding["sha256"]:
                    raise ValueError("unchanged native private bytes required")
                group = 10001 if name in {"app.env", "owner.env"} else 70 if name.endswith(".password") else 0
                os.fchown(file, -1, group)
                os.fchmod(file, 0o640 if group else 0o600)
            finally:
                os.close(file)
        os.fchown(descriptor, -1, 10001)
        os.fchmod(descriptor, 0o750)
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--private-dir", required=True, type=Path)
    parser.add_argument("--admit-native-copy", type=Path)
    args = parser.parse_args()
    if args.admit_native_copy is None:
        prepare(args.private_dir)
        print("Fresh synthetic private inputs prepared; no values published.")
    else:
        admit_native_copy(args.private_dir, args.admit_native_copy)
        print("Native private copy admitted with unchanged bytes; no values published.")


if __name__ == "__main__":
    main()
