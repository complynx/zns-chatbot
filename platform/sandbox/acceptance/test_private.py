import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from urllib.parse import urlsplit

from prepare_private import DATABASE, prepare


class PrivateInputsTest(unittest.TestCase):
    def test_fresh_inputs_have_exact_shared_and_separate_bindings(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            prepare(directory)
            roles = ("postgres", "app", "meter", "inventory", "fake", "operator")
            environments = ("app", "owner", "fake", "operator", "media", "sticker", "roles", "inventory")
            self.assertEqual(
                {role + ".password" for role in roles} | {name + ".env" for name in environments},
                {path.name for path in directory.iterdir()},
            )
            passwords = {role: (directory / (role + ".password")).read_text().strip()
                         for role in roles}
            self.assertEqual(6, len(set(passwords.values())))

            def environment(name):
                return dict(line.split("=", 1) for line in
                            (directory / (name + ".env")).read_text().splitlines())

            app = environment("app")
            fake = environment("fake")
            owner = environment("owner")
            operator = environment("operator")
            for name in ("app", "fake", "owner", "operator"):
                values = environment(name)
                self.assertTrue(app["ZNS_AUTH__SIGNING_KEY"] == values["ZNS_AUTH__SIGNING_KEY"], name + ": signing binding")
                self.assertTrue(app["ZNS_TELEGRAM__TOKEN"] == values["ZNS_TELEGRAM__TOKEN"], name + ": Telegram binding")
            self.assertTrue(app["ZNS_TELEGRAM__TOKEN"].startswith("8123:"))
            self.assertTrue(app["ZNS_DATABASE__URL"] == owner["ZNS_DATABASE__URL"], "owner: app binding")
            self.assertTrue(app["ZNS_DATABASE__URL"] != fake["ZNS_DATABASE__URL"], "fake: separate role")
            self.assertTrue(app["ZNS_DATABASE__URL"] != operator["ZNS_DATABASE__URL"], "operator: separate role")
            for name, role in {"app": "app", "owner": "app", "fake": "fake",
                               "operator": "operator", "media": "meter"}.items():
                values = environment(name)
                url = urlsplit(values["DATABASE_URL" if name == "media" else "ZNS_DATABASE__URL"])
                self.assertEqual("postgres", url.scheme)
                self.assertEqual("postgres", url.hostname)
                self.assertEqual(5432, url.port)
                self.assertEqual("/" + DATABASE, url.path)
                self.assertEqual("sslmode=disable", url.query)
                self.assertEqual("zns_registration_operator" if role == "operator" else "zns_" + role, url.username)
                self.assertTrue(url.password == passwords[role], name + ": password-file binding")
            for name, role in {"roles": "app", "inventory": "inventory"}.items():
                values = environment(name)
                self.assertEqual("postgres", values["PGHOST"])
                self.assertEqual("5432", values["PGPORT"])
                self.assertEqual(DATABASE, values["PGDATABASE"])
                self.assertEqual("zns_" + role, values["PGUSER"])
                self.assertTrue(values["PGPASSWORD"] == passwords[role], name + ": password-file binding")
            self.assertTrue(app["ZNS_MEDIA__SECRET"] == environment("media")["MEDIA_WORKER_SECRET"], "media: secret binding")
            self.assertTrue(app["ZNS_STICKER__WORKER__SECRET"] == environment("sticker")["STICKER_WORKER_SECRET"], "sticker: secret binding")
            self.assertEqual("synthetic-acceptance-no-paid-provider", environment("media")["OPENAI_API_KEY"])
            for name in environments:
                self.assertFalse(any(key.startswith("REGISTRATION_CLOCK_") for key in environment(name)))
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
