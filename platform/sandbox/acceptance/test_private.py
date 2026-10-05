import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from prepare_private import DATABASE, prepare


class PrivateInputsTest(unittest.TestCase):
    def test_fresh_inputs_have_exact_shared_and_separate_bindings(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            prepare(directory)
            self.assertEqual(14, len(list(directory.iterdir())))

            def environment(name):
                return dict(line.split("=", 1) for line in
                            (directory / (name + ".env")).read_text().splitlines())

            app = environment("app")
            fake = environment("fake")
            owner = environment("owner")
            operator = environment("operator")
            for name in ("app", "fake", "owner", "operator"):
                values = environment(name)
                self.assertEqual(app["ZNS_AUTH__SIGNING_KEY"], values["ZNS_AUTH__SIGNING_KEY"])
                self.assertEqual(app["ZNS_TELEGRAM__TOKEN"], values["ZNS_TELEGRAM__TOKEN"])
                self.assertFalse(any(key.startswith("REGISTRATION_CLOCK_") for key in values))
                self.assertIn("@postgres:5432/" + DATABASE + "?sslmode=disable", values["ZNS_DATABASE__URL"])
            self.assertTrue(app["ZNS_TELEGRAM__TOKEN"].startswith("8123:"))
            self.assertEqual(app["ZNS_DATABASE__URL"], owner["ZNS_DATABASE__URL"])
            self.assertNotEqual(app["ZNS_DATABASE__URL"], fake["ZNS_DATABASE__URL"])
            self.assertNotEqual(app["ZNS_DATABASE__URL"], operator["ZNS_DATABASE__URL"])
            self.assertTrue(operator["ZNS_DATABASE__URL"].startswith("postgres://zns_registration_operator:"))
            self.assertEqual(app["ZNS_MEDIA__SECRET"], environment("media")["MEDIA_WORKER_SECRET"])
            self.assertEqual(app["ZNS_STICKER__WORKER__SECRET"], environment("sticker")["STICKER_WORKER_SECRET"])
            self.assertEqual(6, len({(directory / (name + ".password")).read_bytes()
                                     for name in ("postgres", "app", "meter", "inventory", "fake", "operator")}))
            for path in directory.iterdir():
                self.assertEqual(0o600, path.stat().st_mode & 0o777)

    def test_existing_input_is_not_overwritten(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            original = directory / "app.env"
            original.write_bytes(b"original")
            with self.assertRaises(ValueError):
                prepare(directory)
            self.assertEqual(b"original", original.read_bytes())
            self.assertEqual([original], list(directory.iterdir()))

    def test_storage_fault_removes_only_owned_partial_inputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            with patch.object(os, "fsync", side_effect=OSError("controlled durability failure")):
                with self.assertRaises(OSError):
                    prepare(directory)
            self.assertEqual([], list(directory.iterdir()))

    def test_symlink_directory_is_not_followed(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            target = directory / "target"
            target.mkdir()
            link = directory / "link"
            link.symlink_to(target, target_is_directory=True)
            with self.assertRaises(OSError):
                prepare(link)
            self.assertEqual([], list(target.iterdir()))


if __name__ == "__main__":
    unittest.main()
