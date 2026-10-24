"""Derive sqlc's catalog from the single conditional Goose migration."""

from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent


def extract_schema(source: str) -> str:
    markers = (
        "-- BEGIN PRESERVED VERSION 73 DDL\n",
        "-- END PRESERVED VERSION 73 DDL\n",
        "-- BEGIN COMMON VERSION 74 DDL\n",
        "-- END COMMON VERSION 74 DDL\n",
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
    return "\n".join(line for line in (fresh + common).splitlines()
                     if not line.startswith("-- +goose ")) + "\n"


def main() -> None:
    migrations = sorted((ROOT / "migrations").glob("*.sql"))
    if [path.name for path in migrations] != ["00074_schema.sql"]:
        raise ValueError("expected the single version-74 migration")
    source = migrations[0].read_text(encoding="utf-8")
    if len(source) > 1_000_000:
        raise ValueError("unexpected migration size")
    schema = extract_schema(source)
    output = ROOT / ".cache" / "shuttle-sqlc" / "schema.sql"
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(schema, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    main()
