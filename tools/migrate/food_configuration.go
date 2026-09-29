package migrate

import (
	"encoding/json"
	"errors"
	"time"
)

// FoodConfiguration is an exporter-attested effective Python configuration. The
// menu resource digest binds all positional item indices to their original list.
type FoodConfiguration struct {
	Event          string                     `json:"event_key"`
	MenuFile       string                     `json:"menu_file"`
	MenuSHA256     string                     `json:"menu_sha256"`
	Deadline       time.Time                  `json:"deadline"`
	MealPrices     map[string]json.RawMessage `json:"meal_prices"`
	ActivityPrices map[string]json.RawMessage `json:"activity_prices"`
	Capacity       int                        `json:"cacao_capacity"`
	FirstBefore    int64                      `json:"first_before_seconds"`
	LastBefore     int64                      `json:"last_before_seconds"`
	NotifyAfter    int64                      `json:"notify_after_seconds"`
	Admins         []FoodAdmin                `json:"admins"`
}

type FoodAdmin struct {
	Owner        int64             `json:"user_id"`
	Export       bool              `json:"can_export"`
	Review       bool              `json:"can_review"`
	Assign       bool              `json:"can_assign"`
	Instructions map[string]string `json:"instructions"`
}

func convertFoodConfiguration(raw []byte) (*FoodConfiguration, error) {
	f, err := objectFields(
		raw,
		"_id kind event_key menu_file menu_sha256 deadline meal_prices activity_prices cacao_capacity first_before_seconds last_before_seconds notify_after_seconds admins",
	)
	if err != nil {
		return nil, errors.New("food_configuration_field_unmapped")
	}
	var kind string
	if json.Unmarshal(f["kind"], &kind) != nil || kind != "legacy_food" {
		return nil, errors.New("food_configuration_kind_invalid")
	}
	var config FoodConfiguration
	if json.Unmarshal(raw, &config) != nil {
		return nil, errors.New("food_configuration_invalid")
	}
	if !tokenPattern.MatchString(config.Event) || !digestPattern.MatchString(config.MenuSHA256) ||
		config.MenuFile == "" {
		return nil, errors.New("food_menu_identity_required")
	}
	config.Deadline, err = eventInstant(f["deadline"])
	if err != nil {
		return nil, err
	}
	if config.Capacity != 38 || config.FirstBefore <= config.LastBefore || config.LastBefore <= 0 ||
		config.NotifyAfter < 0 {
		return nil, errors.New("food_policy_invalid")
	}
	if err = validateFoodPrices(config); err != nil {
		return nil, err
	}
	if err = validateFoodAdmins(config, f["admins"]); err != nil {
		return nil, err
	}
	return &config, nil
}

func validateFoodPrices(config FoodConfiguration) error {
	for _, prices := range []struct {
		values map[string]json.RawMessage
		keys   []string
	}{
		{config.MealPrices, []string{"with_soup", "without_soup"}},
		{config.ActivityPrices, []string{"party", "party_and_classes", "all_classes", foodYoga, foodCacao, foodSoundHealing}},
	} {
		if len(prices.values) != len(prices.keys) {
			return errors.New("food_prices_invalid")
		}
		for _, key := range prices.keys {
			if _, err := orderMoney(prices.values[key]); err != nil {
				return errors.New("food_prices_invalid")
			}
		}
	}
	return nil
}

func validateFoodAdmins(config FoodConfiguration, raw []byte) error {
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return errors.New("food_admin_invalid")
	}
	if err := validateFoodAdminFields(rows); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, admin := range config.Admins {
		if admin.Owner <= 0 || admin.Owner >= maxTelegramID || seen[admin.Owner] ||
			!admin.Export && !admin.Review && !admin.Assign {
			return errors.New("food_admin_invalid")
		}
		seen[admin.Owner] = true
		for locale, text := range admin.Instructions {
			if locale != "en" && locale != "ru" || len(text) > 4000 {
				return errors.New("food_admin_instructions_invalid")
			}
		}
	}
	return nil
}
func validateFoodAdminFields(rows []json.RawMessage) error {
	for _, raw := range rows {
		fields, err := objectFields(raw, "user_id can_export can_review can_assign instructions")
		if err != nil {
			return errors.New("food_admin_field_unmapped")
		}
		for _, key := range []string{"can_export", "can_review", "can_assign"} {
			var value bool
			if err = foodOptionalBool(fields, key, &value); err != nil {
				return err
			}
		}
	}
	return nil
}
