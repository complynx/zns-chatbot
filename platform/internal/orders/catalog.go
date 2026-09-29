// Package orders owns festival meal and activity orders. Amounts use BYN cents.
package orders

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"unicode/utf8"
)

// Money is stored as integer cents but encoded as decimal currency for the web UI.
type Money int64

const extraTotalKey = "total"

const maxDishCount = 10000
const maxServiceCount = 1000000
const centsPerUnit = 100
const maxMoney Money = 100000000000 // Explicit bound also keeps arithmetic safe.

func (m Money) MarshalJSON() ([]byte, error) {
	if m < 0 || m > maxMoney {
		return nil, fmt.Errorf("amount out of range")
	}
	return []byte(fmt.Sprintf("%d.%02d", m/centsPerUnit, m%centsPerUnit)), nil
}
func (m *Money) UnmarshalJSON(b []byte) error {
	var number json.Number
	if len(b) == 0 || b[0] == '"' || string(b) == "null" {
		return fmt.Errorf("amount must be a number")
	}
	if err := json.Unmarshal(b, &number); err != nil {
		return err
	}
	r, ok := new(big.Rat).SetString(string(number))
	if !ok {
		return fmt.Errorf("invalid amount")
	}
	r.Mul(r, big.NewRat(centsPerUnit, 1))
	if !r.IsInt() || !r.Num().IsInt64() || r.Sign() < 0 || r.Num().Int64() > int64(maxMoney) {
		return fmt.Errorf("invalid cents")
	}
	*m = Money(r.Num().Int64())
	return nil
}

type Definition struct {
	NameRU  string   `json:"name_ru,omitempty"`
	Price   Money    `json:"price"`
	Kind    string   `json:"kind,omitempty"`
	Service []string `json:"service,omitempty"`
}
type Catalog struct {
	Dishes       map[string]Definition                     `json:"dishes"`
	ServiceItems map[string]Definition                     `json:"service_items"`
	Choices      map[string]map[string]map[string][]string `json:"choices"`
}

// MenuJSON is the current Python catalog, without personal data.
//
//go:embed menu_belarus.json
var MenuJSON []byte

func Menu() (Catalog, error) {
	var c Catalog
	err := json.Unmarshal(MenuJSON, &c)
	return c, err
}

type Extra struct {
	Price    Money `json:"price"`
	Capacity int   `json:"capacity,omitempty"`
	Legacy   bool  `json:"legacy,omitempty"`
}

// Extras is the published festival price and capacity table.
//
//nolint:mnd // These literal amounts and capacities are catalog data.
func Extras() map[string]Extra {
	return map[string]Extra{
		"preparty": {Price: 3500}, "excursion_minsk": {Price: 3000},
		"shuttle":                     {Price: 6500, Capacity: 53},
		"excursion_grodno":            {Price: 2500, Legacy: true},
		"excursion_grodno_overview":   {Price: 2500, Capacity: 20},
		"excursion_grodno_gorodnitsa": {Price: 2500, Capacity: 25},
	}
}

// Item accepts client prices for compatibility; Canonicalize always rebuilds them.
type Item struct {
	Name  string          `json:"name"`
	Count int64           `json:"count"`
	Price json.RawMessage `json:"price,omitempty"`
	Total json.RawMessage `json:"total,omitempty"`
}
type MealInput struct {
	Dishes  []Item          `json:"dishes,omitempty"`
	Total   json.RawMessage `json:"total,omitempty"`
	Service json.RawMessage `json:"service,omitempty"`
}
type DayInput struct {
	Mealtimes map[string]MealInput `json:"mealtimes,omitempty"`
	Total     json.RawMessage      `json:"total,omitempty"`
}
type ChoiceInput struct {
	Customer   string                     `json:"customer,omitempty"`
	FirstName  string                     `json:"customer_first_name,omitempty"`
	LastName   string                     `json:"customer_last_name,omitempty"`
	Patronymic string                     `json:"customer_patronymus,omitempty"`
	Days       map[string]DayInput        `json:"days,omitempty"`
	Extras     map[string]json.RawMessage `json:"extras,omitempty"`
	Total      json.RawMessage            `json:"total,omitempty"`
}
type Line struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
	Price Money  `json:"price"`
	Total Money  `json:"total"`
}
type ServiceItems struct {
	Items []Line `json:"items"`
	Total Money  `json:"total"`
}
type Meal struct {
	Dishes  []Line       `json:"dishes"`
	Service ServiceItems `json:"service"`
	Total   Money        `json:"total"`
}
type Day struct {
	Mealtimes map[string]Meal `json:"mealtimes"`
	Total     Money           `json:"total"`
}
type Choice struct {
	Customer   string           `json:"customer,omitempty"`
	FirstName  string           `json:"customer_first_name,omitempty"`
	LastName   string           `json:"customer_last_name,omitempty"`
	Patronymic string           `json:"customer_patronymus,omitempty"`
	Days       map[string]Day   `json:"days"`
	Extras     map[string]Money `json:"extras"`
	Total      Money            `json:"total"`
}

func validName(s string) bool {
	if len(s) > 1024 || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if (r < 32 && r != '\t' && r != '\n' && r != '\r') || r == 0xfffe || r == 0xffff {
			return false
		}
	}
	return true
}

// Canonicalize validates current choices and ignores all client-supplied prices.
func Canonicalize(in ChoiceInput, c Catalog, extras map[string]Extra) (Choice, error) {
	out := Choice{
		Customer:   in.Customer,
		FirstName:  in.FirstName,
		LastName:   in.LastName,
		Patronymic: in.Patronymic,
		Days:       map[string]Day{},
	}
	for _, name := range []string{in.Customer, in.FirstName, in.LastName, in.Patronymic} {
		if !validName(name) {
			return Choice{}, fmt.Errorf("invalid customer name")
		}
	}
	for key, input := range in.Days {
		day, err := c.canonicalDay(key, input)
		if err != nil {
			return Choice{}, err
		}
		out.Days[key] = day
		out.Total += day.Total
		if out.Total > maxMoney {
			return Choice{}, fmt.Errorf("total too large")
		}
	}
	var err error
	out.Extras, err = canonicalExtras(in.Extras, extras)
	if err != nil {
		return Choice{}, err
	}
	out.Total += out.Extras[extraTotalKey]
	if out.Total > maxMoney {
		return Choice{}, fmt.Errorf("total too large")
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return Choice{}, err
	}
	if len(encoded) > maxChoiceBytes {
		return Choice{}, fmt.Errorf("order choice too large")
	}
	return out, nil
}

func (c Catalog) canonicalDay(key string, in DayInput) (Day, error) {
	allowed, exists := c.Choices[key]
	if !exists {
		return Day{}, fmt.Errorf("unknown day %s", key)
	}
	out := Day{Mealtimes: map[string]Meal{}}
	for mealKey, input := range in.Mealtimes {
		categories, existsMeal := allowed[mealKey]
		if !existsMeal {
			return Day{}, fmt.Errorf("unknown meal %s", mealKey)
		}
		meal, err := c.canonicalMeal(input, categories)
		if err != nil {
			return Day{}, err
		}
		out.Mealtimes[mealKey] = meal
		out.Total += meal.Total
		if out.Total > maxMoney {
			return Day{}, fmt.Errorf("total too large")
		}
	}
	return out, nil
}

func (c Catalog) canonicalMeal(in MealInput, categories map[string][]string) (Meal, error) {
	allowed := map[string]bool{}
	for _, names := range categories {
		for _, name := range names {
			allowed[name] = true
		}
	}
	out := Meal{Dishes: []Line{}}
	counts := map[string]int64{}
	for _, item := range in.Dishes {
		definition, exists := c.Dishes[item.Name]
		if !exists || !allowed[item.Name] || item.Count <= 0 || item.Count > maxDishCount {
			return Meal{}, fmt.Errorf("invalid dish %s", item.Name)
		}
		line, err := pricedLine(item.Name, item.Count, definition.Price)
		if err != nil {
			return Meal{}, err
		}
		out.Dishes = append(out.Dishes, line)
		out.Total += line.Total
		if out.Total > maxMoney {
			return Meal{}, fmt.Errorf("total too large")
		}
		if err = c.countServices(definition.Service, item.Count, counts); err != nil {
			return Meal{}, err
		}
	}
	var err error
	out.Service, err = c.priceServices(counts)
	if err != nil {
		return Meal{}, err
	}
	out.Total += out.Service.Total
	if out.Total > maxMoney {
		return Meal{}, fmt.Errorf("total too large")
	}
	return out, nil
}

func (c Catalog) countServices(keys []string, quantity int64, counts map[string]int64) error {
	for _, key := range keys {
		definition, exists := c.ServiceItems[key]
		if !exists {
			return fmt.Errorf("catalog service missing: %s", key)
		}
		if definition.Kind == "utensil" {
			counts[key] = 1
		} else {
			counts[key] += quantity
		}
		if counts[key] > maxServiceCount {
			return fmt.Errorf("service quantity too large")
		}
	}
	return nil
}

func (c Catalog) priceServices(counts map[string]int64) (ServiceItems, error) {
	out := ServiceItems{Items: []Line{}}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		line, err := pricedLine(key, counts[key], c.ServiceItems[key].Price)
		if err != nil {
			return ServiceItems{}, err
		}
		out.Items = append(out.Items, line)
		out.Total += line.Total
		if out.Total > maxMoney {
			return ServiceItems{}, fmt.Errorf("total too large")
		}
	}
	return out, nil
}

func pricedLine(name string, count int64, price Money) (Line, error) {
	if count <= 0 || price < 0 || price > maxMoney/Money(count) {
		return Line{}, fmt.Errorf("invalid price or quantity")
	}
	return Line{Name: name, Count: count, Price: price, Total: price * Money(count)}, nil
}

func canonicalExtras(in map[string]json.RawMessage, definitions map[string]Extra) (map[string]Money, error) {
	out := map[string]Money{extraTotalKey: 0}
	for key := range in {
		if key == extraTotalKey {
			continue
		}
		extra, exists := definitions[key]
		if !exists || extra.Legacy || extra.Price < 0 || extra.Price > maxMoney {
			return nil, fmt.Errorf("invalid extra %s", key)
		}
		out[key] = extra.Price
		out[extraTotalKey] += extra.Price
		if out[extraTotalKey] > maxMoney {
			return nil, fmt.Errorf("total too large")
		}
	}
	_, overview := out["excursion_grodno_overview"]
	_, gorodnitsa := out["excursion_grodno_gorodnitsa"]
	if overview && gorodnitsa {
		return nil, fmt.Errorf("grodno tours are mutually exclusive")
	}
	return out, nil
}
