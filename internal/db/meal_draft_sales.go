package db

import (
	"reflect"
	"slices"
	"time"

	"github.com/complynx/zns-chatbot/internal/orders"
)

// A draft is not an immutable order. Current authorized windows govern edits;
// source windows are used only when a service was removed from the new catalog.
func checkMealDraftChanges(
	source, current orders.MealCatalog,
	before *orders.MealEvaluation,
	after orders.MealEvaluation,
	now time.Time,
) error {
	oldDays := mealDays(source)
	newDays := mealDays(current)
	oldChoices := map[string]orders.MealDayChoice{}
	if before != nil {
		oldChoices = mealChoices(before.Choices)
	}
	newChoices := mealChoices(after.Choices)
	currencyChanged := source.Currency != current.Currency || source.CurrencyScale != current.CurrencyScale
	for id := range oldDays {
		if _, ok := newDays[id]; !ok {
			newDays[id] = orders.MealDay{ID: id}
		}
	}
	for id, day := range newDays {
		if err := checkMealDayChanges(
			oldDays[id],
			day,
			oldChoices[id],
			newChoices[id],
			before,
			after,
			currencyChanged,
			now,
		); err != nil {
			return err
		}
	}

	return nil
}

func checkMealDayChanges(
	old, day orders.MealDay,
	a, b orders.MealDayChoice,
	before *orders.MealEvaluation,
	after orders.MealEvaluation,
	currencyChanged bool,
	now time.Time,
) error {
	id := day.ID
	if a.Lunch != nil || b.Lunch != nil {
		unchanged := !currencyChanged && reflect.DeepEqual(a.Lunch, b.Lunch) &&
			reflect.DeepEqual(old.Lunch, day.Lunch)
		if unchanged {
			if !sameMealLines(before, after, id, "lunch") {
				return ErrConflict
			}
		} else if !mealLunchOpen(old.Lunch, day.Lunch, now) {
			return orders.ErrSalesClosed
		}
	}
	if len(a.DinnerIDs) != 0 || len(b.DinnerIDs) != 0 {
		unchanged := !currencyChanged && slices.Equal(a.DinnerIDs, b.DinnerIDs) &&
			reflect.DeepEqual(old.Dinner, day.Dinner)
		if unchanged {
			if !sameMealLines(before, after, id, "dinner") {
				return ErrConflict
			}
		} else if !mealDinnerOpen(old.Dinner, day.Dinner, now) {
			return orders.ErrSalesClosed
		}
	}
	return nil
}

func mealDays(catalog orders.MealCatalog) map[string]orders.MealDay {
	days := make(map[string]orders.MealDay, len(catalog.Days))
	for _, day := range catalog.Days {
		days[day.ID] = day
	}
	return days
}

func mealChoices(choices []orders.MealDayChoice) map[string]orders.MealDayChoice {
	days := make(map[string]orders.MealDayChoice, len(choices))
	for _, choice := range choices {
		days[choice.DayID] = choice
	}
	return days
}

func mealLunchOpen(before, after *orders.MealLunch, now time.Time) bool {
	if after != nil {
		return after.Service.SalesOpen(now)
	}
	return before != nil && before.Service.SalesOpen(now)
}

func mealDinnerOpen(before, after *orders.MealService, now time.Time) bool {
	if after != nil {
		return after.SalesOpen(now)
	}
	return before != nil && before.SalesOpen(now)
}

func sameMealLines(before *orders.MealEvaluation, after orders.MealEvaluation, day, meal string) bool {
	if before == nil {
		return false
	}
	return slices.Equal(mealLines(*before, day, meal), mealLines(after, day, meal))
}

func mealLines(evaluation orders.MealEvaluation, day, meal string) []orders.MealPriceLine {
	result := []orders.MealPriceLine{}
	for _, line := range evaluation.Lines {
		if line.DayID == day && line.Meal == meal {
			result = append(result, line)
		}
	}
	return result
}
