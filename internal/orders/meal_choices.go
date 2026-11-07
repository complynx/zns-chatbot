package orders

import (
	"errors"
	"slices"
)

var ErrMealChoices = errors.New("invalid meal choices")

type LunchMode string

const (
	LunchNone             LunchMode = "no-lunch"
	LunchIndividual       LunchMode = "individual-items"
	LunchComboWithoutSoup LunchMode = "combo-no-soup"
	LunchComboWithSoup    LunchMode = "combo-with-soup"
)

type LunchChoice struct {
	Mode          LunchMode `json:"mode"`
	IndividualIDs []string  `json:"individual_ids,omitempty"`
	SoupID        string    `json:"soup_id,omitempty"`
	MainID        string    `json:"main_id,omitempty"`
	SideID        string    `json:"side_id,omitempty"`
	SaladID       string    `json:"salad_id,omitempty"`
}

type MealDayChoice struct {
	DayID     string       `json:"day_id"`
	Lunch     *LunchChoice `json:"lunch"`
	DinnerIDs []string     `json:"dinner_ids"`
}

func normalizeMealChoices(catalog MealCatalog, choices []MealDayChoice) ([]MealDayChoice, error) {
	if len(choices) > len(catalog.Days) {
		return nil, ErrMealChoices
	}
	byDay := make(map[string]MealDayChoice, len(choices))
	for _, choice := range choices {
		if _, exists := byDay[choice.DayID]; exists {
			return nil, ErrMealChoices
		}
		byDay[choice.DayID] = choice
	}
	result := make([]MealDayChoice, 0, len(catalog.Days))
	for _, day := range catalog.Days {
		choice := byDay[day.ID]
		choice.DayID = day.ID
		delete(byDay, day.ID)
		if choice.Lunch != nil {
			lunch, err := normalizeLunchChoice(day.Lunch, *choice.Lunch)
			if err != nil {
				return nil, err
			}
			choice.Lunch = &lunch
		}
		var items []MealItem
		if day.Dinner != nil {
			items = day.Dinner.Items
		}
		dinner, err := normalizeMealSet(items, choice.DinnerIDs)
		if err != nil {
			return nil, err
		}
		choice.DinnerIDs = dinner
		result = append(result, choice)
	}
	if len(byDay) != 0 {
		return nil, ErrMealChoices
	}
	return result, nil
}

func normalizeMealSet(items []MealItem, ids []string) ([]string, error) {
	if len(ids) > len(items) {
		return nil, ErrMealChoices
	}
	allowed := make(map[string]bool, len(items))
	for _, item := range items {
		allowed[item.ID] = true
	}
	result := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !allowed[id] || seen[id] {
			return nil, ErrMealChoices
		}
		result = append(result, id)
		seen[id] = true
	}
	slices.Sort(result)
	return result, nil
}

func normalizeLunchChoice(lunch *MealLunch, choice LunchChoice) (LunchChoice, error) {
	if lunch == nil {
		return LunchChoice{}, ErrMealChoices
	}
	hasComboFields := choice.SoupID != "" || choice.MainID != "" || choice.SideID != "" || choice.SaladID != ""
	switch choice.Mode {
	case LunchNone:
		if hasComboFields || len(choice.IndividualIDs) != 0 {
			return LunchChoice{}, ErrMealChoices
		}
	case LunchIndividual:
		if hasComboFields {
			return LunchChoice{}, ErrMealChoices
		}
		ids, err := normalizeMealSet(lunch.Service.Items, choice.IndividualIDs)
		if err != nil {
			return LunchChoice{}, err
		}
		choice.IndividualIDs = ids
	case LunchComboWithoutSoup, LunchComboWithSoup:
		if lunch.Combos == nil || len(choice.IndividualIDs) != 0 ||
			(choice.Mode == LunchComboWithoutSoup && choice.SoupID != "") {
			return LunchChoice{}, ErrMealChoices
		}
		if err := validateMealComponents(lunch.Service.Items, choice); err != nil {
			return LunchChoice{}, err
		}
	default:
		return LunchChoice{}, ErrMealChoices
	}
	// Empty individual sets and omitted sets have the same canonical meaning.
	if len(choice.IndividualIDs) == 0 {
		choice.IndividualIDs = nil
	}
	return choice, nil
}

func validateMealComponents(items []MealItem, choice LunchChoice) error {
	categories := make(map[string]MealCategory, len(items))
	for _, item := range items {
		categories[item.ID] = item.Category
	}
	for _, component := range mealComponents(choice) {
		if component.id == "" {
			continue
		}
		category, ok := categories[component.id]
		if !ok || category != component.category {
			return ErrMealChoices
		}
	}
	return nil
}

type mealComponent struct {
	id       string
	category MealCategory
}

func mealComponents(choice LunchChoice) []mealComponent {
	components := []mealComponent{{choice.MainID, MealMain}, {choice.SideID, MealSide}, {choice.SaladID, MealSalad}}
	if choice.Mode == LunchComboWithSoup {
		components = append([]mealComponent{{choice.SoupID, MealSoup}}, components...)
	}
	return components
}
