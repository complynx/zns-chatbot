"""Pure native wire-shape/fixture tests, no files or external calls."""
import unittest
import archive_upload as archive
import history_fixture as history
import operator_cli as qa


class HistoryTest(unittest.TestCase):
    def test_raw_preparation_accepts_go_nil_uuid_only(self):
        digest = "a" * 64
        archive.raw_arguments({"sha256": digest}, digest)
        archive.raw_arguments({"sha256": digest, "source_file_id": "00000000-0000-0000-0000-000000000000"}, digest)
        for bad in ({"sha256": digest, "source_file_id": "2d68ee4a-3ed6-4303-895b-7e583e594a9a"}, {"sha256": "b" * 64}, {"sha256": digest, "other": True}):
            with self.assertRaises(ValueError):
                archive.raw_arguments(bad, digest)

    def test_fixture_is_explicit_synthetic_history_without_live_actions(self):
        value = history.fixture(qa.BOT, 741123, "314ff086-c8ac-4c0e-9408-9601e25bd352")
        self.assertEqual(set(value), {"users", "messages"})
        self.assertTrue(all(row["content"].startswith("SYNTHETIC QA history:") for row in value["messages"]))


if __name__ == "__main__":
    unittest.main()
