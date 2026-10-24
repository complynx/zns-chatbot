"""Offline checks for the derived sqlc schema; no database or subprocesses."""

import importlib.util
from pathlib import Path
import unittest

ROOT = Path(__file__).resolve().parent.parent
SPEC = importlib.util.spec_from_file_location("prepare_schema", ROOT / "tools/prepare-shuttle-sqlc-schema.py")
PREPARE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PREPARE)


class SchemaExtractionTest(unittest.TestCase):
    def test_real_single_migration_has_full_catalog_and_extension(self):
        source = (ROOT / "migrations/00074_schema.sql").read_text(encoding="utf-8")
        schema = PREPARE.extract_schema(source)
        self.assertEqual(schema.count("CREATE TABLE shuttle_user_profiles ("), 1)
        self.assertEqual(schema.count("ADD COLUMN first_name_latin text"), 1)
        self.assertEqual(schema.count("ADD COLUMN last_name_latin text"), 1)
        self.assertNotIn("DO $shuttle_baseline74$", schema)
        self.assertNotIn("-- +goose", schema)
        self.assertNotIn("DROP TABLE", schema)
        self.assertEqual(schema, PREPARE.extract_schema(source))

    def test_missing_duplicate_reordered_and_empty_sections_fail_closed(self):
        source = (ROOT / "migrations/00074_schema.sql").read_text(encoding="utf-8")
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
