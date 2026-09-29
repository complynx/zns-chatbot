package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"
)

type foodMenuDay struct {
	Lunch  []json.RawMessage `json:"lunch"`
	Dinner []json.RawMessage `json:"dinner"`
}

func validateFoodMenu(raw []byte, orders []FoodPlanRecord) error {
	var menu map[string]foodMenuDay
	if json.Unmarshal(raw, &menu) != nil || len(menu) == 0 {
		return errors.New("food_menu_invalid")
	}
	for key, day := range menu {
		if !tokenPattern.MatchString(key) {
			return errors.New("food_menu_day_invalid")
		}
		for _, list := range [][]json.RawMessage{day.Lunch, day.Dinner} {
			if err := validateFoodDishes(list); err != nil {
				return err
			}
		}
	}
	for _, row := range orders {
		if err := validateFoodMeals(menu, row.Food.Meals); err != nil {
			return err
		}
	}
	return nil
}

func validateFoodDishes(items []json.RawMessage) error {
	for _, item := range items {
		var dish map[string]json.RawMessage
		if json.Unmarshal(item, &dish) != nil {
			return errors.New("food_menu_item_invalid")
		}
		for _, locale := range []string{"title_ru", "title_en"} {
			if _, err := orderString(dish, locale, true); err != nil {
				return errors.New("food_menu_title_invalid")
			}
		}
		if _, err := orderMoney(dish["price"]); err != nil {
			return errors.New("food_menu_price_invalid")
		}
	}
	return nil
}

func validateFoodMeals(menu map[string]foodMenuDay, raw []byte) error {
	var days map[string]json.RawMessage
	if json.Unmarshal(raw, &days) != nil {
		return errors.New("food_meals_invalid")
	}
	for key, day := range days {
		catalog, exists := menu[key]
		if !exists {
			return errors.New("food_menu_day_unresolved")
		}
		if err := validateFoodMealDay(catalog, day); err != nil {
			return err
		}
	}
	return nil
}

func validateFoodMealDay(catalog foodMenuDay, raw []byte) error {
	fields, err := objectFields(raw, "lunch dinner")
	if err != nil {
		return errors.New("food_meal_field_unmapped")
	}
	if value := fields["dinner"]; len(value) > 0 && !bytes.Equal(value, []byte("null")) {
		if err = validateFoodIndices(value, len(catalog.Dinner)); err != nil {
			return err
		}
	}
	if value := fields["lunch"]; len(value) > 0 && !bytes.Equal(value, []byte("null")) {
		return validateFoodLunch(value, len(catalog.Lunch))
	}
	return nil
}

func validateFoodLunch(raw []byte, count int) error {
	fields, err := objectFields(raw, "type items")
	if err != nil {
		return errors.New("food_lunch_field_unmapped")
	}
	kind, err := orderString(fields, "type", true)
	if err != nil {
		return err
	}
	switch kind {
	case "no-lunch":
		return nil
	case "individual-items":
		return validateFoodIndices(fields["items"], count)
	case "combo-with-soup", "combo-no-soup":
		if len(fields["items"]) == 0 || bytes.Equal(fields["items"], []byte("null")) {
			return nil
		}
		items, parseErr := objectFields(fields["items"], "soup_index main_index side_index salad_index")
		if parseErr != nil {
			return errors.New("food_combo_field_unmapped")
		}
		for _, item := range items {
			if !bytes.Equal(item, []byte("null")) {
				if err = validateFoodIndex(item, count); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return errors.New("food_lunch_type_unmapped")
	}
}

func validateFoodIndices(raw []byte, count int) error {
	var indices []json.RawMessage
	if json.Unmarshal(raw, &indices) != nil || indices == nil {
		return errors.New("food_item_list_invalid")
	}
	for _, value := range indices {
		if err := validateFoodIndex(value, count); err != nil {
			return err
		}
	}
	return nil
}

func validateFoodIndex(raw []byte, count int) error {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	index, err := strconv.Atoi(text)
	if err != nil || index < 0 || index >= count {
		return errors.New("food_menu_index_unresolved")
	}
	return nil
}
