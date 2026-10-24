package orders

import "encoding/json"

type MealMissingChoice struct {
	DayID string `json:"day_id"`
	Field string `json:"field"`
}

type MealPriceLine struct {
	DayID       string    `json:"day_id"`
	Meal        string    `json:"meal"`
	Mode        LunchMode `json:"mode,omitempty"`
	ItemID      string    `json:"item_id,omitempty"`
	AmountMinor int64     `json:"amount_minor"`
}

type MealQuote struct {
	Currency      string `json:"currency"`
	CurrencyScale int    `json:"currency_scale"`
	TotalMinor    int64  `json:"total_minor"`
}

type MealEvaluation struct {
	Choices          []MealDayChoice     `json:"choices"`
	Complete         bool                `json:"complete"`
	Missing          []MealMissingChoice `json:"missing"`
	Lines            []MealPriceLine     `json:"lines"`
	KnownAmountMinor int64               `json:"known_amount_minor"`
	Quote            *MealQuote          `json:"quote,omitempty"`
}

// EvaluateMeals validates and copies composition, including incomplete drafts.
// KnownAmountMinor excludes incomplete combos. Only complete composition has a
// quote; neither result asserts sale eligibility, payment or a submitted order.
func EvaluateMeals(document MealCatalog, choices []MealDayChoice) (MealEvaluation, error) {
	catalog, err := NormalizeMeals(document)
	if err != nil {
		return MealEvaluation{}, err
	}
	canonical, err := normalizeMealChoices(catalog, choices)
	if err != nil {
		return MealEvaluation{}, err
	}
	result := MealEvaluation{Choices: canonical, Missing: []MealMissingChoice{}, Lines: []MealPriceLine{}}
	for i, day := range catalog.Days {
		choice := canonical[i]
		if day.Lunch != nil {
			if err = evaluateLunch(&result, day.ID, *day.Lunch, choice.Lunch); err != nil {
				return MealEvaluation{}, err
			}
		}
		if day.Dinner != nil {
			if err = appendMealItems(&result, day.ID, "dinner", "", day.Dinner.Items, choice.DinnerIDs); err != nil {
				return MealEvaluation{}, err
			}
		}
	}
	result.Complete = len(result.Missing) == 0
	if result.Complete {
		result.Quote = &MealQuote{
			Currency:      catalog.Currency,
			CurrencyScale: catalog.CurrencyScale,
			TotalMinor:    result.KnownAmountMinor,
		}
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > MaxQuoteBytes {
		return MealEvaluation{}, ErrMealChoices
	}
	return result, nil
}

func evaluateLunch(result *MealEvaluation, dayID string, lunch MealLunch, choice *LunchChoice) error {
	if choice == nil {
		result.Missing = append(result.Missing, MealMissingChoice{DayID: dayID, Field: "lunch"})
		return nil
	}
	switch choice.Mode {
	case LunchNone:
		return nil
	case LunchIndividual:
		return appendMealItems(result, dayID, "lunch", choice.Mode, lunch.Service.Items, choice.IndividualIDs)
	case LunchComboWithoutSoup, LunchComboWithSoup:
		complete := true
		for _, component := range mealComponents(*choice) {
			if component.id == "" {
				result.Missing = append(
					result.Missing,
					MealMissingChoice{DayID: dayID, Field: string(component.category)},
				)
				complete = false
			}
		}
		if !complete {
			return nil
		}
		amount := lunch.Combos.WithoutSoupMinor
		if choice.Mode == LunchComboWithSoup {
			amount = lunch.Combos.WithSoupMinor
		}
		return appendMealLine(
			result,
			MealPriceLine{DayID: dayID, Meal: "lunch", Mode: choice.Mode, AmountMinor: amount},
		)
	default:
		return ErrMealChoices
	}
}

func appendMealItems(result *MealEvaluation, dayID, meal string, mode LunchMode, items []MealItem, ids []string) error {
	prices := make(map[string]int64, len(items))
	for _, item := range items {
		prices[item.ID] = item.PriceMinor
	}
	for _, id := range ids {
		if err := appendMealLine(
			result,
			MealPriceLine{DayID: dayID, Meal: meal, Mode: mode, ItemID: id, AmountMinor: prices[id]},
		); err != nil {
			return err
		}
	}
	return nil
}

func appendMealLine(result *MealEvaluation, line MealPriceLine) error {
	if err := addAmount(&result.KnownAmountMinor, line.AmountMinor); err != nil {
		return err
	}
	result.Lines = append(result.Lines, line)
	return nil
}
