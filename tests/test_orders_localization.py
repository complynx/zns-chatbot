import ast
import re
import unittest
from pathlib import Path

from fluent.runtime import FluentLocalization, FluentResourceLoader


PROJECT_ROOT = Path(__file__).resolve().parents[1]


class OrdersLocalizationTests(unittest.TestCase):
    def setUp(self):
        loader = FluentResourceLoader(str(PROJECT_ROOT / "i18n" / "{locale}"))
        self.localization = FluentLocalization(["en"], ["bot.ftl"], loader)

    def test_payment_review_messages_are_available_in_english(self):
        messages = {
            "food-payment-proof-confirmed": {"name": "Alice"},
            "food-payment-proof-rejected": {"name": "Alice"},
            "food-adm-payment-proof-confirmed": {
                "link": "User",
                "name": "Alice",
            },
            "food-adm-payment-proof-rejected": {
                "link": "User",
                "name": "Alice",
            },
        }

        for key, arguments in messages.items():
            with self.subTest(key=key):
                value = self.localization.format_value(key, arguments)
                self.assertNotEqual(value, key)
                self.assertIn("Alice", value)

    def test_literal_localization_keys_used_by_code_exist_in_both_languages(self):
        localization_keys = {}
        for locale in ("ru", "en"):
            source = (PROJECT_ROOT / "i18n" / locale / "bot.ftl").read_text(
                encoding="utf-8"
            )
            localization_keys[locale] = set(
                re.findall(r"(?m)^([a-zA-Z][a-zA-Z0-9_-]*)\s*=", source)
            )

        calls = {"l", "loc", "localization"}
        for path in (PROJECT_ROOT / "zns-chatbot").rglob("*.py"):
            tree = ast.parse(path.read_text(encoding="utf-8-sig"), filename=str(path))
            for node in ast.walk(tree):
                if not isinstance(node, ast.Call) or not node.args:
                    continue
                if isinstance(node.func, ast.Attribute):
                    function_name = node.func.attr
                elif isinstance(node.func, ast.Name):
                    function_name = node.func.id
                else:
                    continue
                first_argument = node.args[0]
                if (
                    function_name not in calls
                    or not isinstance(first_argument, ast.Constant)
                    or not isinstance(first_argument.value, str)
                ):
                    continue

                key = first_argument.value
                for locale in ("ru", "en"):
                    with self.subTest(path=path.name, line=node.lineno, key=key, locale=locale):
                        self.assertIn(key, localization_keys[locale])


if __name__ == "__main__":
    unittest.main()
