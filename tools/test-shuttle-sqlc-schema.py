"""Offline checks for the derived sqlc schema; no database or subprocesses."""

import importlib.util
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location("prepare_schema", ROOT / "tools/prepare-shuttle-sqlc-schema.py")
PREPARE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PREPARE)


class SchemaExtractionTest(unittest.TestCase):
    def test_additive_migrations_extend_catalog_in_order_without_down(self):
        baseline = ROOT / "migrations/00076_schema.sql"
        with tempfile.TemporaryDirectory() as directory:
            first = Path(directory) / "00077_extra.sql"
            first.write_text("-- +goose Up\n-- +goose StatementBegin\nCREATE TABLE extra (id bigint);\n"
                             "-- +goose StatementEnd\n-- +goose Down\nDROP TABLE extra;\n")
            second = Path(directory) / "00078_extra_column.sql"
            second.write_text("-- +goose Up\nALTER TABLE extra ADD COLUMN value text;\n-- +goose Down\nDROP TABLE extra;\n")
            schema = PREPARE.derive_schema([second, baseline, first])
        self.assertLess(schema.index("CREATE TABLE extra"), schema.index("ALTER TABLE extra"))
        self.assertIn("CREATE TABLE shuttle_user_profiles", schema)
        self.assertNotIn("DROP TABLE extra", schema)
        self.assertNotIn("-- +goose", schema)

    def test_additive_sections_fail_closed(self):
        for source in (
            "-- +goose Up\nCREATE TABLE extra(id bigint);\n",
            "-- +goose Down\n-- +goose Up\nCREATE TABLE extra(id bigint);\n",
            "-- +goose Up\n-- +goose Down\n",
            "-- +goose Up\n-- +goose Up\nCREATE TABLE extra(id bigint);\n-- +goose Down\n",
            "-- +goose Up\n-- +goose StatementBegin\nCREATE TABLE extra(id bigint);\n-- +goose Down\n",
        ):
            with self.subTest(source=source):
                with self.assertRaises(ValueError):
                    PREPARE.extract_additive_up(source)

    def test_missing_baseline_and_duplicate_versions_fail_closed(self):
        baseline = ROOT / "migrations/00076_schema.sql"
        for paths in ([], [Path("00077_extra.sql")],
                      [baseline, Path("00075_old.sql")],
                      [baseline, Path("00077_first.sql"), Path("00077_second.sql")],
                      [baseline, Path("unexpected.sql")]):
            with self.subTest(paths=paths):
                with self.assertRaises(ValueError):
                    PREPARE.derive_schema(paths)

    def test_current_migrations_include_ban_table_without_rollback(self):
        schema = PREPARE.derive_schema(list((ROOT / "migrations").glob("*.sql")))
        self.assertEqual(schema.count("CREATE TABLE shuttle_user_bans ("), 1)
        self.assertNotIn("DROP TABLE shuttle_user_bans", schema)
        self.assertNotIn("user ban audit must be preserved", schema)

    def test_real_single_migration_has_full_catalog_and_extension(self):
        source = (ROOT / "migrations/00076_schema.sql").read_text(encoding="utf-8")
        schema = PREPARE.extract_schema(source)
        self.assertEqual(schema.count("CREATE TABLE shuttle_user_profiles ("), 1)
        self.assertEqual(schema.count("ADD COLUMN first_name_latin text"), 1)
        self.assertEqual(schema.count("ADD COLUMN last_name_latin text"), 1)
        self.assertNotIn("DO $shuttle_baseline74$", schema)
        self.assertNotIn("-- +goose", schema)
        self.assertNotIn("DROP TABLE", schema)
        self.assertEqual(schema, PREPARE.extract_schema(source))

    def test_missing_duplicate_reordered_and_empty_sections_fail_closed(self):
        source = (ROOT / "migrations/00076_schema.sql").read_text(encoding="utf-8")
        marker = "-- BEGIN COMMON VERSION 74 DDL\n"
        end = "-- END COMMON VERSION 74 DDL\n"
        empty = source[:source.index(marker) + len(marker)] + source[source.index(end):]
        reversed_markers = source.replace(marker, "TEMP_MARKER\n").replace(end, marker).replace("TEMP_MARKER\n", end)
        for broken in (source.replace(marker, ""), source + marker,
                       reversed_markers, empty,
                       source.replace("CREATE TABLE shuttle_user_profiles (", "CREATE TABLE missing_profile ("),
                       source.replace("ADD COLUMN first_name_latin text", "ADD COLUMN missing_latin text")):
            with self.subTest(source=broken[:40]):
                with self.assertRaises(ValueError):
                    PREPARE.extract_schema(broken)


if __name__ == "__main__":
    unittest.main()
