import json
import unittest
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]


class OrdersMenuCatalogTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.menu = json.loads(
            (PROJECT_ROOT / "static" / "menu_belarus.json").read_text(
                encoding="utf-8"
            )
        )

    def test_every_meal_has_trout_sandwich_and_both_breads(self):
        meals_checked = 0
        for day_menu in self.menu["choices"].values():
            for categories in day_menu.values():
                meals_checked += 1
                self.assertIn("trout_sandwich", categories["bread"])
                self.assertIn("black_bread", categories["bread"])
                self.assertIn("white_bread", categories["bread"])

        self.assertEqual(meals_checked, 5)

    def test_all_salads_have_onion_free_ingredients(self):
        salad_keys = {
            dish_key
            for day_menu in self.menu["choices"].values()
            for categories in day_menu.values()
            for dish_key in categories["salads"]
        }

        self.assertEqual(len(salad_keys), 8)
        for dish_key in salad_keys:
            with self.subTest(dish=dish_key):
                dish = self.menu["dishes"][dish_key]
                self.assertTrue(dish["ingredients_ru"])
                self.assertTrue(dish["ingredients_en"])
                self.assertNotIn("лук", dish["ingredients_ru"].casefold())

    def test_all_menu_images_exist(self):
        photo_directory = PROJECT_ROOT / "static" / "orders_photos"
        for dish_key, dish in self.menu["dishes"].items():
            if image := dish.get("image"):
                with self.subTest(dish=dish_key):
                    self.assertTrue((photo_directory / image).is_file())

    def test_juices_are_separate_in_every_meal(self):
        for day_menu in self.menu["choices"].values():
            for categories in day_menu.values():
                with self.subTest(drinks=categories["drinks"]):
                    self.assertIn("orange_juice", categories["drinks"])
                    self.assertIn("apple_juice", categories["drinks"])
                    self.assertNotIn("orange_apple_juice", categories["drinks"])

    def test_content_icons_have_no_question_marks(self):
        for content_key, content in self.menu["content_icons"].items():
            with self.subTest(content=content_key):
                self.assertNotIn("?", content["icon"])
                self.assertNotIn("?", content["ru"])
                self.assertNotIn("?", content["en"])

    def test_spring_vegetables_have_ingredients(self):
        dish = self.menu["dishes"]["spring_vegetables"]
        self.assertEqual(
            dish["ingredients_ru"],
            "цветная капуста, стручковая фасоль, морковь, зелёный горошек и брюссельская капуста",
        )


if __name__ == "__main__":
    unittest.main()
