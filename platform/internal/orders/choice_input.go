package orders

import "encoding/json"

// Input copies a priced choice into editable selections without carrying prices.
func (choice Choice) Input() ChoiceInput {
	in := ChoiceInput{
		Customer:   choice.Customer,
		FirstName:  choice.FirstName,
		LastName:   choice.LastName,
		Patronymic: choice.Patronymic,
		Days:       map[string]DayInput{},
		Extras:     map[string]json.RawMessage{},
	}
	for key := range choice.Extras {
		if key != extraTotalKey {
			in.Extras[key] = json.RawMessage(`0`)
		}
	}
	for dayKey, day := range choice.Days {
		input := DayInput{Mealtimes: map[string]MealInput{}}
		for mealKey, meal := range day.Mealtimes {
			items := []Item{}
			for _, item := range meal.Dishes {
				items = append(items, Item{Name: item.Name, Count: item.Count})
			}
			input.Mealtimes[mealKey] = MealInput{Dishes: items}
		}
		in.Days[dayKey] = input
	}
	return in
}
