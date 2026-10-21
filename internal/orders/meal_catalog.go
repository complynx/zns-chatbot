package orders

import (
	"encoding/json"
	"errors"
	"time"
)

const MaxMealDays = 14

var ErrMealCatalog = errors.New("invalid meal catalog")

type MealCategory string

const (
	MealSoup  MealCategory = "soup"
	MealMain  MealCategory = "main"
	MealSide  MealCategory = "side"
	MealSalad MealCategory = "salad"
)

type MealItem struct {
	ID         string       `json:"id"`
	Name       Label        `json:"name"`
	Category   MealCategory `json:"category,omitempty"`
	PriceMinor int64        `json:"price_minor"`
}

type MealService struct {
	StartsAt      time.Time  `json:"starts_at"`
	EndsAt        time.Time  `json:"ends_at"`
	SalesOpensAt  time.Time  `json:"sales_opens_at"`
	SalesClosesAt time.Time  `json:"sales_closes_at"`
	Items         []MealItem `json:"items"`
}

type MealComboPrices struct {
	WithoutSoupMinor int64 `json:"without_soup_minor"`
	WithSoupMinor    int64 `json:"with_soup_minor"`
}

type MealLunch struct {
	Service MealService      `json:"service"`
	Combos  *MealComboPrices `json:"combos,omitempty"`
}

type MealDay struct {
	ID     string       `json:"id"`
	Name   Label        `json:"name"`
	Lunch  *MealLunch   `json:"lunch,omitempty"`
	Dinner *MealService `json:"dinner,omitempty"`
}

// MealCatalog uses final item and combo prices, including mandatory charges.
// Catalogs requiring additional charges are not supported by this version.
type MealCatalog struct {
	PaymentDeadline *time.Time `json:"payment_deadline,omitempty"`
	Currency        string     `json:"currency"`
	CurrencyScale   int        `json:"currency_scale"`
	Days            []MealDay  `json:"days"`
}

// NormalizeMeals copies a bounded catalog with explicit instants and integer
// prices. The owning event supplies its IANA zone; weekday names imply no dates.
func NormalizeMeals(document MealCatalog) (MealCatalog, error) {
	if !currency.MatchString(document.Currency) || document.CurrencyScale < 0 ||
		document.CurrencyScale > MaxCurrencyScale ||
		len(document.Days) < 1 ||
		len(document.Days) > MaxMealDays {
		return MealCatalog{}, ErrMealCatalog
	}
	result := document
	if document.PaymentDeadline != nil {
		if !validInstant(*document.PaymentDeadline) {
			return MealCatalog{}, ErrMealCatalog
		}
		deadline := document.PaymentDeadline.UTC()
		result.PaymentDeadline = &deadline
	}
	result.Days = make([]MealDay, len(document.Days))
	seen := make(map[string]bool, len(document.Days))
	for i, day := range document.Days {
		if seen[day.ID] {
			return MealCatalog{}, ErrMealCatalog
		}
		normalized, err := normalizeMealDay(day)
		if err != nil {
			return MealCatalog{}, err
		}
		result.Days[i] = normalized
		seen[day.ID] = true
	}

	raw, err := json.Marshal(result)
	if err != nil || len(raw) > MaxDocumentBytes {
		return MealCatalog{}, ErrMealCatalog
	}
	return result, nil
}

func normalizeMealDay(day MealDay) (MealDay, error) {
	if !identifier.MatchString(day.ID) || (day.Lunch == nil && day.Dinner == nil) {
		return MealDay{}, ErrMealCatalog
	}
	name, err := normalizeLabel(day.Name)
	if err != nil {
		return MealDay{}, ErrMealCatalog
	}
	day.Name = name
	if day.Lunch != nil {
		lunch, lunchErr := normalizeMealLunch(*day.Lunch)
		if lunchErr != nil {
			return MealDay{}, lunchErr
		}
		day.Lunch = &lunch
	}
	if day.Dinner != nil {
		dinner, dinnerErr := normalizeMealService(*day.Dinner)
		if dinnerErr != nil {
			return MealDay{}, dinnerErr
		}
		day.Dinner = &dinner
	}
	return day, nil
}

func normalizeMealService(service MealService) (MealService, error) {
	if !validInstant(service.StartsAt) || !validInstant(service.EndsAt) || !validInstant(service.SalesOpensAt) ||
		!validInstant(service.SalesClosesAt) ||
		!service.EndsAt.After(service.StartsAt) ||
		!service.SalesClosesAt.After(service.SalesOpensAt) ||
		len(service.Items) < 1 ||
		len(service.Items) > MaxItems {
		return MealService{}, ErrMealCatalog
	}
	service.Items = append([]MealItem(nil), service.Items...)
	seen := make(map[string]bool, len(service.Items))
	for i, item := range service.Items {
		if !identifier.MatchString(item.ID) || seen[item.ID] || !validMealPrice(item.PriceMinor) ||
			!validMealCategory(item.Category) {
			return MealService{}, ErrMealCatalog
		}
		name, err := normalizeLabel(item.Name)
		if err != nil {
			return MealService{}, ErrMealCatalog
		}
		service.Items[i].Name = name
		seen[item.ID] = true
	}
	service.StartsAt, service.EndsAt = service.StartsAt.UTC(), service.EndsAt.UTC()
	service.SalesOpensAt, service.SalesClosesAt = service.SalesOpensAt.UTC(), service.SalesClosesAt.UTC()
	return service, nil
}

func normalizeMealLunch(lunch MealLunch) (MealLunch, error) {
	service, err := normalizeMealService(lunch.Service)
	if err != nil {
		return MealLunch{}, err
	}
	lunch.Service = service
	if lunch.Combos == nil {
		return lunch, nil
	}
	prices := *lunch.Combos
	if !validMealPrice(prices.WithoutSoupMinor) || !validMealPrice(prices.WithSoupMinor) {
		return MealLunch{}, ErrMealCatalog
	}
	categories := make(map[MealCategory]bool)
	for _, item := range service.Items {
		categories[item.Category] = true
	}
	for _, category := range []MealCategory{MealSoup, MealMain, MealSide, MealSalad} {
		if !categories[category] {
			return MealLunch{}, ErrMealCatalog
		}
	}
	lunch.Combos = &prices
	return lunch, nil
}

func validMealCategory(category MealCategory) bool {
	switch category {
	case "", MealSoup, MealMain, MealSide, MealSalad:
		return true
	default:
		return false
	}
}

func validMealPrice(amount int64) bool { return amount >= 0 && amount <= MaxPrice }

// SalesOpen is inclusive at opening and exclusive at closing. Draft reads and
// evaluation do not imply current permission to change a service's choices.
func (s MealService) SalesOpen(now time.Time) bool {
	return !now.Before(s.SalesOpensAt) && now.Before(s.SalesClosesAt)
}
