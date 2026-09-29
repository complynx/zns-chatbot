package migrate

import (
	"encoding/json"
	"errors"
)

func orderDayTotal(raw json.RawMessage) (int64, error) {
	f, err := objectFields(raw, "mealtimes total")
	if err != nil {
		return 0, errors.New("order_meal_mapping_required")
	}
	var meals map[string]json.RawMessage
	if json.Unmarshal(f["mealtimes"], &meals) != nil || meals == nil {
		return 0, errors.New("order_meal_mapping_required")
	}
	var sum int64
	for _, meal := range meals {
		value, mealErr := orderMealTotal(meal)
		if mealErr != nil {
			return 0, mealErr
		}
		sum += value
		if sum > maxOrderMoney {
			return 0, errors.New("order_money_invalid")
		}
	}
	return orderCheckTotal(f[orderTotalKey], sum)
}
func orderMealTotal(raw json.RawMessage) (int64, error) {
	f, err := objectFields(raw, "dishes service total")
	if err != nil {
		return 0, errors.New("order_meal_mapping_required")
	}
	dishes, err := orderLinesTotal(f["dishes"])
	if err != nil {
		return 0, err
	}
	service, err := objectFields(f["service"], "items total")
	if err != nil {
		return 0, errors.New("order_meal_mapping_required")
	}
	items, err := orderLinesTotal(service["items"])
	if err != nil {
		return 0, err
	}
	if _, err = orderCheckTotal(service[orderTotalKey], items); err != nil {
		return 0, err
	}
	return orderCheckTotal(f[orderTotalKey], dishes+items)
}
func orderCheckTotal(raw json.RawMessage, sum int64) (int64, error) {
	total, err := orderMoney(raw)
	if err != nil {
		return 0, err
	}
	if total != sum {
		return 0, errors.New("order_total_inconsistent")
	}
	return total, nil
}
func orderLinesTotal(raw json.RawMessage) (int64, error) {
	var lines []json.RawMessage
	if json.Unmarshal(raw, &lines) != nil || lines == nil {
		return 0, errors.New("order_meal_mapping_required")
	}
	var sum int64
	for _, line := range lines {
		f, err := objectFields(line, "name count price total")
		if err != nil {
			return 0, errors.New("order_meal_mapping_required")
		}
		if _, err = orderString(f, "name", true); err != nil {
			return 0, err
		}
		var count int64
		if json.Unmarshal(f["count"], &count) != nil || count <= 0 || count > maxOrderCount {
			return 0, errors.New("order_count_invalid")
		}
		price, err := orderMoney(f["price"])
		if err != nil {
			return 0, err
		}
		total, err := orderCheckTotal(f[orderTotalKey], price*count)
		if err != nil {
			return 0, err
		}
		sum += total
		if sum > maxOrderMoney {
			return 0, errors.New("order_money_invalid")
		}
	}
	return sum, nil
}
func validateOrderMenu(raw json.RawMessage) error {
	f, err := objectFields(raw, "dishes service_items choices category_labels content_icons")
	if err != nil {
		return errors.New("order_menu_mapping_required")
	}
	for _, key := range []string{"dishes", "service_items"} {
		if err = validateOrderMenuItems(f[key]); err != nil {
			return err
		}
	}
	var choices map[string]map[string]map[string][]string
	if json.Unmarshal(f["choices"], &choices) != nil || choices == nil {
		return errors.New("order_menu_mapping_required")
	}
	for _, key := range []string{"category_labels", "content_icons"} {
		if value, exists := f[key]; exists {
			var labels map[string]map[string]string
			if json.Unmarshal(value, &labels) != nil || labels == nil {
				return errors.New("order_menu_mapping_required")
			}
		}
	}
	return nil
}
func validateOrderMenuItems(raw json.RawMessage) error {
	var items map[string]json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil {
		return errors.New("order_menu_mapping_required")
	}
	var err error
	for _, item := range items {
		fields, itemErr := objectFields(
			item,
			"name_ru name_en ingredients_ru ingredients_en output price contents service image kind icon",
		)
		if itemErr != nil {
			return errors.New("order_menu_mapping_required")
		}
		if _, err = orderMoney(fields["price"]); err != nil {
			return err
		}
		for _, textKey := range []string{"name_ru", "name_en", "ingredients_ru", "ingredients_en", "output", "image", "kind", "icon"} {
			if _, err = orderString(fields, textKey, false); err != nil {
				return err
			}
		}
		if err = validateOrderMenuArrays(fields); err != nil {
			return err
		}
	}
	return nil
}
func validateOrderMenuArrays(fields map[string]json.RawMessage) error {
	for _, arrayKey := range []string{"contents", "service"} {
		if value, exists := fields[arrayKey]; exists {
			var strings []string
			if json.Unmarshal(value, &strings) != nil || strings == nil {
				return errors.New("order_menu_mapping_required")
			}
		}
	}
	return nil
}
