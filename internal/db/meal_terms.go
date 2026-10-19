package db

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/orders"
)

// MaxMealTermsBytes leaves space for a future enclosing operation receipt.
const MaxMealTermsBytes = 48 << 10

var ErrMealTerms = errors.New("invalid complete meal draft terms")

// MealTerms describes a complete composition, not sale permission or an order.
// Each service keeps its own sales window; no common financial cutoff is inferred.
type MealTerms struct {
	EventID        uuid.UUID              `json:"event_id"`
	EventVersion   int64                  `json:"event_version"`
	CatalogVersion int64                  `json:"catalog_version"`
	DraftVersion   int64                  `json:"draft_version"`
	EventName      string                 `json:"event_name"`
	Timezone       string                 `json:"timezone"`
	Choices        []orders.MealDayChoice `json:"choices"`
	Days           []MealDayTerms         `json:"days"`
	Lines          []orders.MealPriceLine `json:"lines"`
	Quote          orders.MealQuote       `json:"quote"`
}

type MealDayTerms struct {
	ID     string            `json:"id"`
	Name   orders.Label      `json:"name"`
	Lunch  *MealServiceTerms `json:"lunch,omitempty"`
	Dinner *MealServiceTerms `json:"dinner,omitempty"`
}

// MealServiceTerms retains selected names and roles. Only MealTerms.Lines carry charges;
// combo components must not be charged again at individual menu prices.
type MealServiceTerms struct {
	StartsAt      time.Time       `json:"starts_at"`
	EndsAt        time.Time       `json:"ends_at"`
	SalesOpensAt  time.Time       `json:"sales_opens_at"`
	SalesClosesAt time.Time       `json:"sales_closes_at"`
	Items         []MealTermsItem `json:"items"`
}

type MealTermsItem struct {
	ID       string              `json:"id"`
	Name     orders.Label        `json:"name"`
	Category orders.MealCategory `json:"category,omitempty"`
}

// FreezeMealDraft copies exact descriptive terms from a retained catalog and
// saved draft with matching lineage. It recomputes the evaluation, does not read
// the latest catalog or clock, and creates no proposal, order, capacity or money.
// Zero and explicitly empty complete choices remain facts, not sale policy.
func FreezeMealDraft(catalog MealCatalogView, draft MealDraft) (MealTerms, error) {
	if !mealTermsLineage(catalog, draft) {
		return MealTerms{}, ErrMealTerms
	}
	document, err := orders.NormalizeMeals(catalog.Document)
	if err != nil {
		return MealTerms{}, ErrMealTerms
	}
	evaluation, err := orders.EvaluateMeals(document, draft.Evaluation.Choices)
	if err != nil || !evaluation.Complete || evaluation.Quote == nil {
		return MealTerms{}, ErrMealTerms
	}
	if !sameMealEvaluation(evaluation, *draft.Evaluation) {
		return MealTerms{}, ErrMealTerms
	}
	result := MealTerms{
		EventID: catalog.EventID, EventVersion: catalog.EventVersion,
		CatalogVersion: catalog.Version, DraftVersion: draft.Version,
		EventName: catalog.EventName, Timezone: catalog.Timezone,
		Choices: evaluation.Choices, Lines: evaluation.Lines, Quote: *evaluation.Quote,
		Days: make([]MealDayTerms, 0, len(document.Days)),
	}
	for i, day := range document.Days {
		choice := evaluation.Choices[i]
		frozen := MealDayTerms{ID: day.ID, Name: day.Name}
		if day.Lunch != nil {
			frozen.Lunch = freezeMealService(day.Lunch.Service, mealLunchIDs(choice.Lunch))
		}
		if day.Dinner != nil {
			frozen.Dinner = freezeMealService(*day.Dinner, choice.DinnerIDs)
		}
		result.Days = append(result.Days, frozen)
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > MaxMealTermsBytes {
		return MealTerms{}, ErrMealTerms
	}
	return result, nil
}

func mealTermsLineage(c MealCatalogView, d MealDraft) bool {
	if c.EventID == uuid.Nil || c.EventID != d.EventID || c.EventVersion < 1 ||
		c.EventVersion != d.EventVersion || c.Version < 1 || c.Version != d.CatalogVersion ||
		d.Version < 1 || d.Cleared || d.Evaluation == nil {
		return false
	}
	return mealTermsContext(c) && c.Timezone == d.Timezone
}

func mealTermsContext(c MealCatalogView) bool {
	if strings.TrimSpace(c.EventName) == "" || !utf8.ValidString(c.EventName) ||
		utf8.RuneCountInString(c.EventName) > events.MaxNameLength || c.Timezone == "" ||
		c.Timezone == "Local" || len(c.Timezone) > events.MaxModuleIDLength {
		return false
	}
	_, err := time.LoadLocation(c.Timezone)
	return err == nil
}

func sameMealEvaluation(a, b orders.MealEvaluation) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	return err == nil && bytes.Equal(left, right)
}

func mealLunchIDs(choice *orders.LunchChoice) []string {
	switch choice.Mode {
	case orders.LunchIndividual:
		return choice.IndividualIDs
	case orders.LunchComboWithSoup:
		return []string{choice.SoupID, choice.MainID, choice.SideID, choice.SaladID}
	case orders.LunchComboWithoutSoup:
		return []string{choice.MainID, choice.SideID, choice.SaladID}
	case orders.LunchNone:
		return nil
	default:
		return nil
	}
}

func freezeMealService(service orders.MealService, ids []string) *MealServiceTerms {
	selected := make(map[string]bool, len(ids))
	for _, id := range ids {
		selected[id] = true
	}
	result := &MealServiceTerms{
		StartsAt: service.StartsAt, EndsAt: service.EndsAt,
		SalesOpensAt: service.SalesOpensAt, SalesClosesAt: service.SalesClosesAt,
		Items: make([]MealTermsItem, 0, len(ids)),
	}
	for _, item := range service.Items {
		if selected[item.ID] {
			result.Items = append(result.Items, MealTermsItem{ID: item.ID, Name: item.Name, Category: item.Category})
		}
	}
	return result
}
