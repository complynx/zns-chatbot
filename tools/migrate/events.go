package migrate

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"time"
)

const eventsSource = "events"
const maxEventTiers = 10000
const maxEventYear = 9999
const maxEventPrice = 1000000000
const maxEventAmount = 1000000

// EventCandidate is the supported pass catalog, before operator policy attestation.
type EventCandidate struct {
	Configuration           EventConfiguration `json:"configuration"`
	ID                      string             `json:"id"`
	FinishesAt              time.Time          `json:"finishes_at"`
	PassportRequired        bool               `json:"passport_required"`
	AssignmentRule          string             `json:"assignment_rule"`
	DisableConcurrencyLimit bool               `json:"disable_concurrency_limit"`
	Titles                  map[string]string  `json:"titles"`
	Tiers                   []EventTier        `json:"tiers"`
	Admins                  []EventAdmin       `json:"admins"`
}

type EventTier struct {
	Amount        int       `json:"amount"`
	Price         int       `json:"price"`
	StartsAt      time.Time `json:"starts_at"`
	Promo         bool      `json:"promo"`
	BlockedByDate bool      `json:"blocked_by_date"`
}

type EventAdmin struct {
	TelegramID int64 `json:"telegram_id"`
	Hidden     bool  `json:"hidden"`
}

type EventPlanRecord struct {
	Legacy    UserLegacyReference `json:"legacy"`
	Candidate *EventCandidate     `json:"candidate"`
	Blockers  []string            `json:"blockers"`
}

func convertEvent(record map[string]json.RawMessage) EventPlanRecord {
	result := EventPlanRecord{Blockers: []string{"event_policy_attestation_required"}}
	candidate, err := supportedEvent(record)
	if err != nil {
		result.Blockers = append(result.Blockers, err.Error())
		return result
	}
	result.Candidate = &candidate
	return result
}

func supportedEvent(record map[string]json.RawMessage) (EventCandidate, error) {
	var candidate EventCandidate
	if json.Unmarshal(record["key"], &candidate.ID) != nil || !tokenPattern.MatchString(candidate.ID) {
		return candidate, errors.New("event_key_invalid")
	}
	if err := supportedEventFields(record); err != nil {
		return candidate, err
	}
	var err error
	candidate.Configuration, err = eventConfiguration(record, candidate.ID)
	if err != nil {
		return candidate, err
	}
	if raw := record["finish_date"]; len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		candidate.Configuration.OpenEnded = true
		candidate.FinishesAt = eventMaximumInstant()
	} else {
		candidate.FinishesAt, err = eventInstant(raw)
	}
	if err != nil {
		return candidate, err
	}
	candidate.AssignmentRule = "distributed"
	if raw, ok := record["pass_assignment_rule"]; ok {
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &candidate.AssignmentRule) != nil {
			return candidate, errors.New("event_assignment_invalid")
		}
		candidate.AssignmentRule = strings.ToLower(strings.TrimSpace(candidate.AssignmentRule))
		if candidate.AssignmentRule != "distributed" && candidate.AssignmentRule != "paired" {
			return candidate, errors.New("event_assignment_invalid")
		}
	}
	if err = eventBool(record, "require_passport", &candidate.PassportRequired); err != nil {
		return candidate, err
	}
	if err = eventBool(record, "disable_max_concurrent_assignments", &candidate.DisableConcurrencyLimit); err != nil {
		return candidate, err
	}
	candidate.Titles, err = eventTitles(record["title_long"], candidate.ID)
	if err != nil {
		return candidate, err
	}
	candidate.Tiers, err = eventTiers(record["pass_types"])
	if err != nil {
		return candidate, err
	}
	candidate.Admins, err = eventAdmins(record)
	return candidate, err
}

func supportedEventFields(record map[string]json.RawMessage) error {
	allowed := strings.Fields(
		"_id key finish_date require_passport pass_assignment_rule disable_max_concurrent_assignments title_long title_short pass_types payment_admin hidden_payment_admins country_emoji thread_channel thread_id thread_locale price amount_cap_per_role",
	)
	for key := range record {
		if slices.Contains(allowed, key) {
			continue
		}
		return errors.New("event_field_unmapped")
	}
	return nil
}

func eventBool(fields map[string]json.RawMessage, key string, target *bool) error {
	raw, present := fields[key]
	if !present {
		return nil
	}
	if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) {
		return errors.New("event_boolean_invalid")
	}
	return json.Unmarshal(raw, target)
}

func eventInstant(raw json.RawMessage) (time.Time, error) {
	var text string
	if len(raw) > 0 && raw[0] == '{' {
		fields, err := objectFields(raw, "$date")
		if err != nil {
			return time.Time{}, errors.New("event_datetime_unresolved")
		}
		raw = fields["$date"]
	}
	if json.Unmarshal(raw, &text) != nil {
		return time.Time{}, errors.New("event_datetime_unresolved")
	}
	value, err := strictEventInstant(text)
	value = value.UTC()
	if err != nil || value.Year() < 1 || value.Year() > maxEventYear || value.Nanosecond()%int(time.Microsecond) != 0 {
		return time.Time{}, errors.New("event_datetime_unresolved")
	}
	return value, nil
}

// Require a lossless RFC3339 spelling before converting to the database instant.
// Go's parser accepts out-of-range offsets and discards fractions beyond nanos.
func strictEventInstant(text string) (time.Time, error) {
	const secondsLength = len("2006-01-02T15:04:05")
	const offsetLength = len("+00:00")
	const maxFractionLength = len(".123456")
	end := len(text) - 1
	layout := "2006-01-02T15:04:05"
	zoneLayout := "Z07:00"
	if !strings.HasSuffix(text, "Z") {
		end = len(text) - offsetLength
		zoneLayout = "-07:00"
		if end < secondsLength || text[end+1:end+3] >= "24" || text[end+4:] >= "60" {
			return time.Time{}, errors.New("event_datetime_unresolved")
		}
	}
	if end < secondsLength {
		return time.Time{}, errors.New("event_datetime_unresolved")
	}
	fraction := text[secondsLength:end]
	if fraction != "" {
		if len(fraction) < 2 || len(fraction) > maxFractionLength || fraction[0] != '.' ||
			strings.Trim(fraction[1:], "0123456789") != "" {
			return time.Time{}, errors.New("event_datetime_unresolved")
		}
		layout += "." + strings.Repeat("0", len(fraction)-1)
	}
	value, err := time.Parse(time.RFC3339Nano, text)
	if err != nil || value.Format(layout+zoneLayout) != text {
		return time.Time{}, errors.New("event_datetime_unresolved")
	}
	return value, nil
}

func eventTitles(raw json.RawMessage, fallback string) (map[string]string, error) {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return map[string]string{"default": fallback, "en": fallback, "ru": fallback}, nil
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		value = strings.TrimSpace(value)
		if value == "" {
			value = fallback
		}
		if !validLinkText(value) {
			return nil, errors.New("event_title_invalid")
		}
		return map[string]string{"default": value, "en": value, "ru": value}, nil
	}
	return eventObjectTitles(raw, fallback)
}

func eventObjectTitles(raw json.RawMessage, fallback string) (map[string]string, error) {
	var fields map[string]string
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, errors.New("event_title_invalid")
	}
	result := make(map[string]string, len(fields))
	decoder := json.NewDecoder(bytes.NewReader(raw))
	_, _ = decoder.Token()
	firstKey := ""
	for decoder.More() {
		token, _ := decoder.Token()
		locale, _ := token.(string)
		var titleValue *string
		if decoder.Decode(&titleValue) != nil || titleValue == nil {
			return nil, errors.New("event_title_invalid")
		}
		title := *titleValue
		key := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(locale)), "_", "-")
		title = strings.TrimSpace(title)
		if key == "" || title == "" {
			continue
		}
		if !tokenPattern.MatchString(key) || !validLinkText(title) {
			return nil, errors.New("event_title_invalid")
		}
		if firstKey == "" {
			firstKey = key
		}
		result[key] = title
	}
	if len(result) == 0 {
		result["default"] = fallback
	}
	if result["en"] == "" && result["ru"] == "" && result["default"] == "" {
		result["default"] = result[firstKey]
	}
	return eventPresentationTitles(result)
}

func eventPresentationTitles(original map[string]string) (map[string]string, error) {
	result := maps.Clone(original)
	for _, locale := range []string{"en", "ru"} {
		keys := []string{locale}
		var regional []string
		for key := range original {
			if strings.HasPrefix(key, locale+"-") {
				regional = append(regional, key)
			}
		}
		slices.Sort(regional)
		keys = append(keys, regional...)
		keys = append(keys, "en", "ru", "default")
		for _, key := range keys {
			if original[key] != "" {
				result[locale] = original[key]
				break
			}
		}
		if result[locale] == "" {
			return nil, errors.New("event_title_fallback_unresolved")
		}
	}
	return result, nil
}

func eventTiers(raw json.RawMessage) ([]EventTier, error) {
	var rows []json.RawMessage
	if len(raw) == 0 {
		return []EventTier{}, nil
	}
	if json.Unmarshal(raw, &rows) != nil || bytes.Equal(raw, []byte("null")) || len(rows) > maxEventTiers {
		return nil, errors.New("event_tiers_unresolved")
	}
	result := make([]EventTier, 0, len(rows))
	for _, row := range rows {
		tier, err := eventTier(row)
		if err != nil {
			return nil, err
		}
		result = append(result, tier)
	}
	return result, nil
}

func eventTier(raw json.RawMessage) (EventTier, error) {
	var tier EventTier
	fields, err := objectFields(raw, "amount price start promo blocked_by_date")
	if err != nil {
		return tier, errors.New("event_tier_invalid")
	}
	if amount, ok := fields["amount"]; ok &&
		(bytes.Equal(amount, []byte("null")) || json.Unmarshal(amount, &tier.Amount) != nil) {
		return tier, errors.New("event_tier_invalid")
	}
	if json.Unmarshal(fields["price"], &tier.Price) != nil || tier.Price < 1 || tier.Price > maxEventPrice ||
		tier.Amount < 0 ||
		tier.Amount > maxEventAmount {
		return tier, errors.New("event_tier_invalid")
	}
	if startRaw, ok := fields["start"]; ok {
		tier.StartsAt, err = eventInstant(startRaw)
	} else {
		tier.StartsAt = eventMaximumInstant()
	}
	if err != nil {
		return tier, err
	}
	if err = eventBool(fields, "promo", &tier.Promo); err != nil {
		return tier, err
	}
	if err = eventBool(fields, "blocked_by_date", &tier.BlockedByDate); err != nil {
		return tier, err
	}
	return tier, nil
}

func eventAdmins(record map[string]json.RawMessage) ([]EventAdmin, error) {
	result := []EventAdmin{}
	seen := map[int64]bool{}
	for _, key := range []string{"payment_admin", "hidden_payment_admins"} {
		raw := record[key]
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			continue
		}
		rows := []json.RawMessage{raw}
		if raw[0] == '[' && json.Unmarshal(raw, &rows) != nil {
			return nil, errors.New("event_admin_invalid")
		}
		for _, row := range rows {
			id, ok := telegramNumber(row)
			if !ok || seen[id] {
				return nil, errors.New("event_admin_invalid")
			}
			seen[id] = true
			result = append(result, EventAdmin{TelegramID: id, Hidden: key == "hidden_payment_admins"})
		}
	}
	slices.SortFunc(result, func(a, b EventAdmin) int { return cmp.Compare(a.TelegramID, b.TelegramID) })
	return result, nil
}
