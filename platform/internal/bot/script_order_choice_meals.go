package bot

import (
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const modernChoicePatchItems = 1024

func validModernMealPatch(changes []modernChoiceMeal) error {
	count := len(changes)
	for _, change := range changes {
		count += len(change.Dishes)
	}
	if count > modernChoicePatchItems {
		return errors.New("too many meal changes")
	}
	return nil
}

type modernChoiceMeal struct {
	Day    int                `json:"day_ref"`
	Meal   int                `json:"meal_ref"`
	Dishes []modernChoiceDish `json:"dishes,omitempty"`
	Append bool               `json:"append,omitempty"`
	Remove bool               `json:"remove,omitempty"`
}
type modernChoiceDish struct {
	Ref   int   `json:"ref"`
	Count int64 `json:"count"`
}
type modernChoiceLabel struct {
	Ref     int    `json:"ref"`
	Label   string `json:"label"`
	Partial bool   `json:"label_partial"`
}
type modernChoiceMealLabel struct {
	modernChoiceLabel

	Dishes []modernChoiceLabel `json:"dishes"`
}
type modernChoiceDayLabel struct {
	modernChoiceLabel

	Meals []modernChoiceMealLabel `json:"meals"`
}

func choiceLabel(index int, key string) modernChoiceLabel {
	label := []rune(key)
	partial := len(label) > modernChoiceLabelRunes
	if partial {
		label = label[:modernChoiceLabelRunes]
	}
	return modernChoiceLabel{Ref: index, Label: string(label), Partial: partial}
}

func patchModernChoiceMeals(choice *orders.ChoiceInput, menu orders.Catalog, changes []modernChoiceMeal) error {
	if choice.Days == nil {
		choice.Days = map[string]orders.DayInput{}
	}
	days := slices.Sorted(maps.Keys(menu.Choices))
	dishes := slices.Sorted(maps.Keys(menu.Dishes))
	for _, change := range changes {
		if change.Day < 0 || change.Day >= len(days) {
			return errors.New("unknown day reference")
		}
		dayKey := days[change.Day]
		meals := slices.Sorted(maps.Keys(menu.Choices[dayKey]))
		if change.Meal == -1 {
			if len(change.Dishes) > 0 || change.Append {
				return errors.New("invalid day patch")
			}
			if change.Remove {
				delete(choice.Days, dayKey)
			} else if _, exists := choice.Days[dayKey]; !exists {
				choice.Days[dayKey] = orders.DayInput{}
			}
			continue
		}
		if change.Meal < 0 || change.Meal >= len(meals) {
			return errors.New("unknown meal reference")
		}
		if err := patchModernChoiceMeal(choice, dayKey, meals[change.Meal], dishes, change); err != nil {
			return err
		}
	}
	return nil
}

func patchModernChoiceMeal(
	choice *orders.ChoiceInput,
	dayKey, mealKey string,
	dishes []string,
	change modernChoiceMeal,
) error {
	day := choice.Days[dayKey]
	if day.Mealtimes == nil {
		day.Mealtimes = map[string]orders.MealInput{}
	}
	meal := day.Mealtimes[mealKey]
	if change.Remove {
		if len(change.Dishes) > 0 || change.Append {
			return errors.New("invalid meal removal")
		}
		delete(day.Mealtimes, mealKey)
		choice.Days[dayKey] = day
		return nil
	}
	if !change.Append {
		meal = orders.MealInput{}
	}
	for _, item := range change.Dishes {
		if item.Ref < 0 || item.Ref >= len(dishes) {
			return errors.New("unknown dish reference")
		}
		meal.Dishes = append(meal.Dishes, orders.Item{Name: dishes[item.Ref], Count: item.Count})
	}
	day.Mealtimes[mealKey] = meal
	choice.Days[dayKey] = day
	return nil
}

func modernChoiceMealCatalog(event orders.Event) ([]modernChoiceDayLabel, error) {
	var menu orders.Catalog
	if err := json.Unmarshal(event.Menu, &menu); err != nil {
		return nil, err
	}
	dishKeys := slices.Sorted(maps.Keys(menu.Dishes))
	refs := map[string]int{}
	for index, key := range dishKeys {
		refs[key] = index
	}
	days := []modernChoiceDayLabel{}
	for dayIndex, dayKey := range slices.Sorted(maps.Keys(menu.Choices)) {
		day := modernChoiceDayLabel{modernChoiceLabel: choiceLabel(dayIndex, dayKey), Meals: []modernChoiceMealLabel{}}
		for mealIndex, mealKey := range slices.Sorted(maps.Keys(menu.Choices[dayKey])) {
			meal := modernChoiceMealLabel{
				modernChoiceLabel: choiceLabel(mealIndex, mealKey),
				Dishes:            []modernChoiceLabel{},
			}
			selected := map[string]bool{}
			for _, names := range menu.Choices[dayKey][mealKey] {
				for _, name := range names {
					selected[name] = true
				}
			}
			for _, name := range slices.Sorted(maps.Keys(selected)) {
				meal.Dishes = append(meal.Dishes, choiceLabel(refs[name], name))
			}
			day.Meals = append(day.Meals, meal)
		}
		days = append(days, day)
	}
	return days, nil
}
