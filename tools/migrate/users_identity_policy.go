package migrate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

const identityUserRecord = "user"

type IdentityPolicy struct {
	Version    int                  `json:"version"`
	PlanSHA256 string               `json:"plan_sha256"`
	Reviewed   bool                 `json:"reviewed"`
	Users      []IdentityPolicyUser `json:"users"`
}

type IdentityPolicyUser struct {
	LegacyKey string `json:"legacy_key"`
	CanBook   *bool  `json:"can_book"`
}

type identityCandidate struct {
	key      string
	canBook  bool
	telegram identityprovision.Telegram
}

func decodeIdentityPolicy(raw []byte, planHash string) (map[string]bool, error) {
	if validJSON(raw) != nil {
		return nil, errors.New("identity_policy_invalid")
	}
	fields, err := objectFields(raw, "version plan_sha256 reviewed users")
	if err != nil {
		return nil, errors.New("identity_policy_invalid")
	}
	var rows []json.RawMessage
	if json.Unmarshal(fields["users"], &rows) != nil {
		return nil, errors.New("identity_policy_invalid")
	}
	for _, row := range rows {
		if _, err = objectFields(row, "legacy_key can_book"); err != nil {
			return nil, errors.New("identity_policy_invalid")
		}
	}
	var policy IdentityPolicy
	if json.Unmarshal(raw, &policy) != nil || policy.Version != 1 || !policy.Reviewed || policy.PlanSHA256 != planHash {
		return nil, errors.New("identity_policy_attestation_required")
	}
	result := make(map[string]bool, len(policy.Users))
	for _, row := range policy.Users {
		if _, exists := result[row.LegacyKey]; exists || row.CanBook == nil {
			return nil, errors.New("identity_policy_invalid")
		}
		result[row.LegacyKey] = *row.CanBook
	}
	return result, nil
}

func identityCandidates(plan []byte, policy map[string]bool) ([]identityCandidate, error) {
	result := []identityCandidate{}
	scanner := bufio.NewScanner(bytes.NewReader(plan))
	scanner.Buffer(make([]byte, recordReadBuffer), maxUserPlanBytes)
	for scanner.Scan() {
		var row UserPlanRecord
		if json.Unmarshal(scanner.Bytes(), &row) != nil {
			return nil, errors.New("apply_plan_invalid")
		}
		if row.Kind != identityUserRecord || row.Status == userExcluded {
			continue
		}
		if !resolvableUser(row) {
			return nil, errors.New("apply_record_blocked")
		}
		allowed, found := policy[row.Legacy.Key]
		if !found {
			return nil, errors.New("identity_policy_missing")
		}
		delete(policy, row.Legacy.Key)
		input, err := identityTelegram(row.Candidate)
		if err != nil {
			return nil, err
		}
		result = append(result, identityCandidate{key: row.Legacy.Key, canBook: allowed, telegram: input})
	}
	if scanner.Err() != nil {
		return nil, errors.New("apply_plan_invalid")
	}
	if len(policy) != 0 {
		return nil, errors.New("identity_policy_extra")
	}
	return result, nil
}

func identityTelegram(candidate *UserCandidate) (identityprovision.Telegram, error) {
	input := identityprovision.Telegram{ID: candidate.TelegramID}
	if candidate.FirstName != nil {
		input.FirstName = *candidate.FirstName
	}
	if candidate.LastName != nil {
		input.LastName = *candidate.LastName
	}
	if candidate.Language != nil {
		input.Language = *candidate.Language
	}
	if input.ID <= 0 || input.ID >= maxTelegramID || utf8.RuneCountInString(input.FirstName) > 200 ||
		utf8.RuneCountInString(input.LastName) > 200 || len(input.Language) > 10 {
		return input, errors.New("identity_metadata_invalid")
	}
	return input, nil
}
