package migrate

import (
	"bytes"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const userFieldUnmapped = "unmapped"
const userLanguageField = "language_code"
const userUsernameField = "username"

const maxTelegramID = 4503599627370496
const userExcluded = "excluded_other_bot"
const userCandidate = "candidate"
const maxProfileRunes = 300
const maxLanguageRunes = 64

// UserCandidate is private conversion data, not a database command or grant.
type UserCandidate struct {
	BroadcastFields        map[string]json.RawMessage `json:"broadcast_fields"`
	TelegramID             int64                      `json:"telegram_id"`
	TargetOwner            *string                    `json:"target_owner"`
	ZitadelIssuer          *string                    `json:"zitadel_issuer"`
	ZitadelSubject         *string                    `json:"zitadel_subject"`
	CanBook                *bool                      `json:"can_book"`
	Username               *string                    `json:"username"`
	FirstName              *string                    `json:"first_name"`
	LastName               *string                    `json:"last_name"`
	PrintName              *string                    `json:"print_name"`
	TelegramMetadataUpdate int64                      `json:"telegram_metadata_update"`
	DisplayName            *string                    `json:"display_name"`
	Language               *string                    `json:"language"`
	PresentationLocale     *string                    `json:"presentation_locale"`
	Role                   *string                    `json:"role"`
	LegalName              *string                    `json:"legal_name"`
	Passport               *string                    `json:"passport"`
	Frozen                 bool                       `json:"frozen"`
	LegacyBanned           bool                       `json:"legacy_banned"`
	Notifications          *UserNotifications         `json:"specialist_notifications,omitempty"`
}

type UserNotifications struct {
	NotifyBookings *bool `json:"notify_bookings"`
	NotifyNext     *bool `json:"notify_next"`
}

type UserLegacyReference struct {
	Key          string          `json:"key"`
	Collection   string          `json:"collection"`
	RecordID     json.RawMessage `json:"record_id"`
	File         string          `json:"file"`
	Line         int64           `json:"line"`
	RecordSHA256 string          `json:"record_sha256"`
}

type UserFieldDisposition struct {
	Field       string `json:"field"`
	Disposition string `json:"disposition"`
}

type UserPlanRecord struct {
	DeferredMassage json.RawMessage        `json:"deferred_massage,omitempty"`
	DeferredFood    json.RawMessage        `json:"deferred_food,omitempty"`
	DeferredPasses  json.RawMessage        `json:"deferred_passes,omitempty"`
	Kind            string                 `json:"kind"`
	Legacy          UserLegacyReference    `json:"legacy"`
	Status          string                 `json:"status"`
	Candidate       *UserCandidate         `json:"candidate,omitempty"`
	Fields          []UserFieldDisposition `json:"fields,omitempty"`
	Blockers        []string               `json:"blockers,omitempty"`
}

func convertUser(record map[string]json.RawMessage, botID int64) UserPlanRecord {
	result := UserPlanRecord{Kind: "user", Status: "blocked"}
	sourceBot, ok := telegramNumber(record["bot_id"])
	if !ok {
		result.Blockers = []string{"bot_scope_invalid"}
		return result
	}
	if sourceBot != botID {
		result.Status = userExcluded
		return result
	}
	telegramID, ok := telegramNumber(record["user_id"])
	if !ok {
		result.Blockers = []string{"telegram_id_invalid"}
		return result
	}
	result.Status = userCandidate
	result.Candidate = &UserCandidate{TelegramID: telegramID, BroadcastFields: broadcastFields(record)}
	result.Blockers = []string{"target_identity_unresolved", "eligibility_policy_unresolved"}
	candidate := result.Candidate
	mapUserMetadata(record, &result)
	candidate.DisplayName = candidate.PrintName
	if candidate.DisplayName == nil || *candidate.DisplayName == "" {
		addUserBlocker(&result, "display_name_unresolved")
	}
	candidate.Role = userString(record, "role", maxProfileRunes, &result)
	if candidate.Role != nil && *candidate.Role != "" && *candidate.Role != "leader" && *candidate.Role != "follower" {
		candidate.Role = nil
		addUserBlocker(&result, "profile_role_invalid")
	}
	candidate.LegalName = userString(record, "legal_name", maxProfileRunes, &result)
	if bytes.Equal(record["passport_number"], []byte("null")) {
		empty := ""
		candidate.Passport = &empty
	} else {
		candidate.Passport = userString(record, "passport_number", maxProfileRunes, &result)
	}
	_, candidate.Frozen = record["legal_name_frozen"]
	_, candidate.LegacyBanned = record["banned"]
	if candidate.LegacyBanned {
		addUserBlocker(&result, "ban_policy_unmapped")
	}
	candidate.Language = userString(record, userLanguageField, maxLanguageRunes, &result)
	if candidate.Language != nil {
		candidate.PresentationLocale = plannedLocale(*candidate.Language)
		if candidate.PresentationLocale == nil {
			addUserBlocker(&result, "locale_mapping_required")
		}
	}
	mapUserFields(record, &result)
	if raw, present := record["massage_specialist"]; present {
		candidate.Notifications = mapNotifications(raw, &result)
	}
	return result
}

func telegramNumber(raw json.RawMessage) (int64, bool) {
	canonical, err := recordID(raw)
	if err != nil || !strings.HasPrefix(canonical, "integer:") {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimPrefix(canonical, "integer:"), 10, 64)
	return value, err == nil && value > 0 && value < maxTelegramID
}

func userString(record map[string]json.RawMessage, field string, limit int, result *UserPlanRecord) *string {
	raw, present := record[field]
	value := ""
	if !present {
		return &value
	}
	if field == userLanguageField && bytes.Equal(raw, []byte("null")) {
		return &value
	}
	if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) ||
		utf8.RuneCountInString(
			value,
		) > limit || strings.ContainsRune(value, utf8.RuneError) || strings.ContainsFunc(value, unicode.IsControl) {
		addUserBlocker(result, "invalid_mapped_field")
		return nil
	}
	return &value
}

// Resolve only tested target-locale forms. Preserve every original tag; uncommon
// tags need exact runtime normalization during the later apply design.
func plannedLocale(raw string) *string {
	tag := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), "_", "-"))
	locale := ""
	switch tag {
	case "", "en", "en-us", "en-gb", "pl", "pl-pl", "de", "de-de":
		locale = "en"
	case "ru", "ru-ru", "be", "be-by", "by", "by-by", "uk", "uk-ua", "ua", "ua-ua":
		locale = "ru"
	default:
		return nil
	}
	return &locale
}

func mapUserFields(record map[string]json.RawMessage, result *UserPlanRecord) {
	fields := make([]string, 0, len(record))
	for field := range record {
		fields = append(fields, field)
	}
	slices.Sort(fields)
	for _, field := range fields {
		disposition := "mapped"
		switch field {
		case "_id",
			"bot_id",
			"user_id",
			userLanguageField,
			"role",
			"legal_name",
			"passport_number",
			"legal_name_frozen":
		case "print_name", "first_name", "last_name", userUsernameField:
			continue
		case "banned":
			disposition = "blocked_policy"
		case "massage_specialist":
			disposition = "partial_notification_mapping"
		case "state":
			disposition = "archived_idle_state"
			if !idleUserState(record[field]) {
				disposition = "blocked_active_state"
				addUserBlocker(result, "active_state_unmapped")
			}
		default:
			if broadcastField(field) {
				result.Fields = append(
					result.Fields,
					UserFieldDisposition{Field: field, Disposition: "mapped_broadcast_profile"},
				)
				continue
			}
			disposition = userFieldUnmapped
			addUserBlocker(result, "unmapped_fields")
		}
		result.Fields = append(result.Fields, UserFieldDisposition{Field: field, Disposition: disposition})
	}
}

func idleUserState(raw json.RawMessage) bool {
	var value map[string]json.RawMessage
	if json.Unmarshal(raw, &value) != nil || len(value) != 1 {
		return false
	}
	return bytes.Equal(value["state"], []byte(`""`))
}

func mapNotifications(raw json.RawMessage, result *UserPlanRecord) *UserNotifications {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		addUserBlocker(result, "specialist_shape_invalid")
		return nil
	}
	addUserBlocker(result, "specialist_event_mapping_unresolved")
	prefs := &UserNotifications{
		NotifyBookings: notificationValue(fields, "notify_bookings", result),
		NotifyNext:     notificationValue(fields, "notify_next", result),
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		disposition := "mapped_notification"
		if name != "notify_bookings" && name != "notify_next" {
			disposition = userFieldUnmapped
			addUserBlocker(result, "unmapped_fields")
		}
		result.Fields = append(
			result.Fields,
			UserFieldDisposition{Field: "massage_specialist." + name, Disposition: disposition},
		)
	}
	return prefs
}

func notificationValue(fields map[string]json.RawMessage, name string, result *UserPlanRecord) *bool {
	value := true
	if raw, present := fields[name]; present {
		if bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil {
			addUserBlocker(result, "notification_value_invalid")
			return nil
		}
	}
	return &value
}

func addUserBlocker(result *UserPlanRecord, code string) {
	if !slices.Contains(result.Blockers, code) {
		result.Blockers = append(result.Blockers, code)
	}
}
