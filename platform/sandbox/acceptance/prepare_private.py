#!/usr/bin/env python3
"""Create fresh synthetic credentials in an approved empty private directory."""

import argparse
import os
from pathlib import Path
import secrets


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


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--private-dir", required=True, type=Path)
    prepare(parser.parse_args().private_dir)
    print("Fresh synthetic private inputs prepared; no values published.")


if __name__ == "__main__":
    main()
