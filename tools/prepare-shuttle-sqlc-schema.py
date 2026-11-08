"""Derive sqlc's catalog from the conditional baseline and additive Goose DDL."""

from pathlib import Path
import re

ROOT = Path(__file__).resolve().parent.parent


def extract_schema(source: str) -> str:
    markers = (
        "-- BEGIN PRESERVED VERSION 73 DDL\n",
        "-- END PRESERVED VERSION 73 DDL\n",
        "-- BEGIN COMMON VERSION 74 DDL\n",
        "-- END COMMON VERSION 74 DDL\n",
        "-- BEGIN COMMON VERSION 75 DDL\n",
        "-- END COMMON VERSION 75 DDL\n",
        "-- BEGIN COMMON VERSION 76 DDL\n",
        "-- END COMMON VERSION 76 DDL\n",
    )
    if any(source.count(marker) != 1 for marker in markers):
        raise ValueError("expected exactly one fresh and one common schema section")
    positions = [source.index(marker) for marker in markers]
    if positions != sorted(positions):
        raise ValueError("schema sections are out of order")
    fresh = source[positions[0] + len(markers[0]):positions[1]]
    common = source[positions[2] + len(markers[2]):positions[3]]
    if not fresh.strip() or not common.strip():
        raise ValueError("schema sections must not be empty")
    if "CREATE TABLE shuttle_user_profiles (" not in fresh:
        raise ValueError("fresh profile schema is missing")
    if "ADD COLUMN first_name_latin text" not in common or "ADD COLUMN last_name_latin text" not in common:
        raise ValueError("common Latin schema is missing")
    # Goose directives delimit PL/pgSQL for Goose; sqlc parses PostgreSQL SQL.
    extra = source[positions[4] + len(markers[4]):positions[5]]
    if "CREATE TABLE shuttle_symbols (" not in extra:
        raise ValueError("visual symbol schema is missing")
    reactions = source[positions[6] + len(markers[6]):positions[7]]
    if "CREATE TABLE shuttle_telegram_reactions (" not in reactions:
        raise ValueError("reaction schema is missing")
    return "\n".join(line for line in (fresh + common + extra + reactions).splitlines()
                     if not line.startswith("-- +goose ")) + "\n"


def extract_additive_up(source: str) -> str:
    lines = source.splitlines()
    up, down = "-- +goose Up", "-- +goose Down"
    if lines.count(up) != 1 or lines.count(down) != 1:
        raise ValueError("expected exactly one Goose Up and Down section")
    start, end = lines.index(up), lines.index(down)
    if start != 0 or end <= start:
        raise ValueError("Goose sections are out of order")
    statements = []
    in_statement = False
    for line in lines[start + 1:end]:
        if line == "-- +goose StatementBegin" and not in_statement:
            in_statement = True
        elif line == "-- +goose StatementEnd" and in_statement:
            in_statement = False
        elif line.startswith("-- +goose "):
            raise ValueError("unexpected Goose directive in additive schema")
        else:
            statements.append(line)
    if in_statement or not any(line.strip() and not line.lstrip().startswith("--") for line in statements):
        raise ValueError("additive Up section must contain complete SQL")
    return "\n".join(statements).rstrip() + "\n"


def derive_schema(migrations: list[Path]) -> str:
    versions = []
    for path in migrations:
        match = re.fullmatch(r"([0-9]{5})_[a-z0-9_]+\.sql", path.name)
        if not match:
            raise ValueError("unexpected migration filename")
        versions.append((int(match[1]), path))
    versions.sort(key=lambda item: item[0])
    if not versions or versions[0][1].name != "00076_schema.sql" or len({v for v, _ in versions}) != len(versions):
        raise ValueError("expected version-76 baseline followed by unique additive migrations")
    schemas = []
    for version, path in versions:
        source = path.read_text(encoding="utf-8")
        if len(source) > 1_000_000:
            raise ValueError("unexpected migration size")
        schemas.append(extract_schema(source) if version == 76 else extract_additive_up(source))
    return "\n".join(schemas)


def main() -> None:
    schema = derive_schema(list((ROOT / "migrations").glob("*.sql")))
    output = ROOT / ".cache" / "shuttle-sqlc" / "schema.sql"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(schema, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    main()
