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


if __name__ == "__main__":
    unittest.main()
