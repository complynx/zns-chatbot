// Package legacyfood owns the independent meal and activity payment lifecycles
// of imported Python food records. Currency is RUB; amounts use integer kopecks.
package legacyfood

import (
	"bytes"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// Amount reuses the exact decimal codec, without inheriting the orders currency.
type Amount = orders.Money

type Dish struct {
	TitleRU  string `json:"title_ru"`
	TitleEN  string `json:"title_en"`
	Price    Amount `json:"price"`
	Category string `json:"category"`
}

type Day struct {
	Lunch  []Dish `json:"lunch"`
	Dinner []Dish `json:"dinner"`
}

type Menu map[string]Day

type MealSelection map[string]DaySelection

type DaySelection struct {
	Lunch  *LunchSelection `json:"lunch,omitempty"`
	Dinner []Index         `json:"dinner,omitempty"`
}

type LunchSelection struct {
	Type  string          `json:"type"`
	Items json.RawMessage `json:"items,omitempty"`
}

// Index accepts the numeric and string indices emitted by the original menu.
type Index int

func (i *Index) UnmarshalJSON(raw []byte) error {
	var text string
	if json.Unmarshal(raw, &text) != nil {
		text = string(raw)
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < 0 {
		return errors.New("food_invalid_item_index")
	}
	*i = Index(value)
	return nil
}

type MealQuote struct {
	Total    Amount `json:"total"`
	Complete bool   `json:"complete"`
}

type MealPrices struct {
	WithSoup    Amount `json:"with_soup"`
	WithoutSoup Amount `json:"without_soup"`
}

// QuoteMeals implements source menu completeness and prices. Catalogue indices
// are validated instead of silently discarding an unrepresentable selection.
func QuoteMeals(menu Menu, prices MealPrices, selection MealSelection) (MealQuote, error) {
	quote := MealQuote{Complete: true}
	for day := range selection {
		if _, ok := menu[day]; !ok {
			return quote, errors.New("food_unknown_menu_day")
		}
	}
	for day, catalog := range menu {
		chosen := selection[day]
		if len(catalog.Lunch) > 0 {
			total, complete, err := quoteLunch(catalog.Lunch, prices, chosen.Lunch)
			if err != nil {
				return quote, err
			}
			quote.Total += total
			quote.Complete = quote.Complete && complete
		}
		total, err := itemTotal(catalog.Dinner, chosen.Dinner)
		if err != nil {
			return quote, err
		}
		quote.Total += total
	}
	if quote.Total > maxTotal {
		return quote, errors.New("food_total_limit")
	}
	return quote, nil
}

const maxTotal Amount = 100000000000
const lunchNone = "no-lunch"
const lunchIndividual = "individual-items"
const lunchSoup = "combo-with-soup"
const lunchWithoutSoup = "combo-no-soup"

func quoteLunch(menu []Dish, prices MealPrices, lunch *LunchSelection) (Amount, bool, error) {
	if lunch == nil {
		return 0, false, nil
	}
	switch lunch.Type {
	case lunchNone:
		return 0, true, nil
	case lunchIndividual:
		var indices []Index
		if len(lunch.Items) > 0 &&
			(bytes.Equal(lunch.Items, []byte("null")) || json.Unmarshal(lunch.Items, &indices) != nil) {
			return 0, false, errors.New("food_invalid_lunch_items")
		}
		total, err := itemTotal(menu, indices)
		return total, true, err
	case lunchSoup, lunchWithoutSoup:
		return quoteCombo(menu, prices, lunch)
	default:
		return 0, false, errors.New("food_invalid_lunch_type")
	}
}

func quoteCombo(menu []Dish, prices MealPrices, lunch *LunchSelection) (Amount, bool, error) {
	var indices map[string]*Index
	if len(lunch.Items) == 0 || bytes.Equal(lunch.Items, []byte("null")) {
		return 0, false, nil
	}
	if json.Unmarshal(lunch.Items, &indices) != nil {
		return 0, false, errors.New("food_invalid_combo")
	}
	keys := []string{comboMain, comboSide, comboSalad}
	price := prices.WithoutSoup
	if lunch.Type == lunchSoup {
		keys = append(keys, comboSoup)
		price = prices.WithSoup
	}
	for _, key := range keys {
		index := indices[key]
		if index == nil {
			return 0, false, nil
		}
		if int(*index) >= len(menu) {
			return 0, false, errors.New("food_invalid_item_index")
		}
	}
	return price, true, nil
}

func itemTotal(menu []Dish, indices []Index) (Amount, error) {
	var total Amount
	for _, index := range indices {
		if index < 0 || int(index) >= len(menu) {
			return 0, errors.New("food_invalid_item_index")
		}
		total += menu[index].Price
		if total > maxTotal {
			return 0, errors.New("food_total_limit")
		}
	}
	return total, nil
}
