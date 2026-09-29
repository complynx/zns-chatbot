package legacyfood

import (
	"encoding/json"
	"sort"
	"strconv"
)

func exportItems(day string, menu Day, chosen DaySelection, prices MealPrices) []exportItem {
	items := []exportItem{}
	appendItems := func(meal, category string, catalog []Dish, indices []Index, component bool) {
		items = appendExportItems(items, day, meal, category, catalog, indices, component)
	}
	if chosen.Lunch != nil {
		lunch := chosen.Lunch
		switch lunch.Type {
		case lunchIndividual:
			var indices []Index
			_ = json.Unmarshal(lunch.Items, &indices)
			appendItems("Обед", "individual", menu.Lunch, indices, false)
		case lunchSoup, lunchWithoutSoup:
			price := prices.WithoutSoup
			if lunch.Type == lunchSoup {
				price = prices.WithSoup
			}
			items = append(
				items,
				exportItem{
					Day:   day,
					Meal:  "Обед",
					Key:   "combo:" + lunch.Type,
					Title: comboName(lunch.Type),
					Price: price,
				},
			)
			var fields map[string]*Index
			_ = json.Unmarshal(lunch.Items, &fields)
			for _, key := range []string{comboSoup, comboMain, comboSide, comboSalad} {
				index := fields[key]
				if index != nil && (key != comboSoup || lunch.Type == lunchSoup) {
					appendItems("Обед", key, menu.Lunch, []Index{*index}, true)
				}
			}
		}
	}
	appendItems("Ужин", "dinner", menu.Dinner, chosen.Dinner, false)
	return items
}

func summaryRows(counts map[exportItem]int) [][]string {
	items := make([]exportItem, 0, len(counts))
	for item := range counts {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Day != b.Day {
			return a.Day < b.Day
		}
		if a.Meal != b.Meal {
			return a.Meal < b.Meal
		}
		return a.Key < b.Key
	})
	rows := [][]string{{"День", "Прием пищи", "Название блюда", "Цена", "Количество заказов", "Сумма"}}
	for _, item := range items {
		count := counts[item]
		price, total := amountText(item.Price), amountText(item.Price*Amount(count))
		if item.Component {
			price, total = "Комбо", ""
		}
		rows = append(rows, []string{dayName(item.Day), item.Meal, item.Title, price, strconv.Itoa(count), total})
	}
	return rows
}

func appendExportItems(
	items []exportItem,
	day, meal, category string,
	catalog []Dish,
	indices []Index,
	component bool,
) []exportItem {
	for _, index := range indices {
		if index < 0 || int(index) >= len(catalog) {
			continue
		}
		items = append(
			items,
			exportItem{
				Day:       day,
				Meal:      meal,
				Key:       category + strconv.Itoa(int(index)),
				Title:     catalog[index].TitleRU,
				Price:     catalog[index].Price,
				Component: component,
			},
		)
	}

	return items
}
