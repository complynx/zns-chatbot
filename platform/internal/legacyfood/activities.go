package legacyfood

import (
	"errors"
	"maps"
)

type Activities map[string]bool

func activityNames() []string {
	return []string{"open", activityYoga, activityCacao, activitySoundHealing}
}
func classNames() []string { return []string{activityYoga, activityCacao, activitySoundHealing} }

type ActivityPrices struct {
	Party           Amount `json:"party"`
	PartyAndClasses Amount `json:"party_and_classes"`
	AllClasses      Amount `json:"all_classes"`
	Yoga            Amount `json:"yoga"`
	Cacao           Amount `json:"cacao"`
	SoundHealing    Amount `json:"soundhealing"`
}

func QuoteActivities(prices ActivityPrices, selection Activities) (Amount, error) {
	if err := validActivities(selection); err != nil {
		return 0, err
	}
	classes := 0
	for _, name := range classNames() {
		if selection[name] {
			classes++
		}
	}
	if selection["open"] {
		if classes > 0 {
			return prices.PartyAndClasses, nil
		}
		return prices.Party, nil
	}
	if classes == len(classNames()) {
		return prices.AllClasses, nil
	}
	var total Amount
	for name, price := range map[string]Amount{activityYoga: prices.Yoga, activityCacao: prices.Cacao, activitySoundHealing: prices.SoundHealing} {
		if selection[name] {
			total += price
		}
	}
	return total, nil
}

func validActivities(selection Activities) error {
	for name := range selection {
		switch name {
		case "open", activityYoga, activityCacao, activitySoundHealing:
		default:
			return errors.New("food_unknown_activity")
		}
	}
	return nil
}

// ToggleActivities mirrors source all/classes replacement and cacao selection.
// The caller holds the event lock and checks the independent payment lock.
func ToggleActivities(selection Activities, name string, cacaoAvailable bool) (Activities, error) {
	if err := validActivities(selection); err != nil {
		return nil, err
	}
	result := Activities{}
	maps.Copy(result, selection)
	if name == "all" || name == "classes" {
		result = Activities{}
		names := activityNames()
		if name == "classes" {
			names = classNames()
		}
		for _, activity := range names {
			if activity != activityCacao || cacaoAvailable {
				result[activity] = true
			}
		}
		return result, nil
	}
	if err := validActivities(Activities{name: true}); err != nil {
		return nil, err
	}
	if name == activityCacao && !cacaoAvailable {
		result[name] = false
	} else {
		result[name] = !result[name]
	}
	return result, nil
}
