package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
)

type EventResolution struct {
	LegacyKey    string `json:"legacy_key"`
	DisplayOrder *int32 `json:"display_order"`
}

// EventResolutions attests source instants, effective configuration and admin grants.
// It cannot override conversion blockers or introduce an administrator.
type EventResolutions struct {
	Version               int               `json:"version"`
	PlanSHA256            string            `json:"plan_sha256"`
	DatesVerified         bool              `json:"dates_verified"`
	ConfigurationVerified bool              `json:"configuration_verified"`
	AdminGrantsVerified   bool              `json:"admin_grants_verified"`
	Events                []EventResolution `json:"events"`
}

type preparedEvents struct {
	plan           EventPlan
	planHash       string
	resolutionHash string
	orders         map[string]int32
}

func prepareEvents(stage, planPath, resolutionsPath string, limits Limits) (preparedEvents, error) {
	var prepared preparedEvents
	plan, generated, err := preparedEventPlan(stage, limits)
	if err != nil {
		return prepared, err
	}
	original, err := readApplyFile(planPath, maxUserPlanBytes)
	if err != nil {
		return prepared, err
	}
	if !bytes.Equal(original, generated) {
		return prepared, errors.New("apply_plan_mismatch")
	}
	raw, err := readApplyFile(resolutionsPath, maxUserResolutionBytes)
	if err != nil {
		return prepared, err
	}
	resolved, err := decodeEventResolutions(raw)
	if err != nil {
		return prepared, err
	}
	prepared = preparedEvents{
		plan:           plan,
		planHash:       hashBytes(original),
		resolutionHash: hashBytes(raw),
		orders:         map[string]int32{},
	}
	if resolved.PlanSHA256 != prepared.planHash {
		return prepared, errors.New("resolution_plan_mismatch")
	}
	for _, row := range resolved.Events {
		prepared.orders[row.LegacyKey] = *row.DisplayOrder
	}
	if len(plan.Events) != len(prepared.orders) {
		return prepared, errors.New("event_resolution_set_mismatch")
	}
	for _, row := range plan.Events {
		if row.Candidate == nil || len(row.Blockers) != 1 || row.Blockers[0] != "event_policy_attestation_required" {
			return prepared, errors.New("apply_record_blocked")
		}
		if _, ok := prepared.orders[row.Legacy.Key]; !ok {
			return prepared, errors.New("resolution_missing")
		}
	}
	return prepared, nil
}

func decodeEventResolutions(raw []byte) (EventResolutions, error) {
	var result EventResolutions
	if validJSON(raw) != nil {
		return result, errors.New("resolution_invalid")
	}
	fields, err := objectFields(
		raw,
		"version plan_sha256 dates_verified configuration_verified admin_grants_verified events",
	)
	if err != nil {
		return result, errors.New("resolution_invalid")
	}
	var rows []json.RawMessage
	if json.Unmarshal(fields["events"], &rows) != nil {
		return result, errors.New("resolution_invalid")
	}
	for _, row := range rows {
		if _, err = objectFields(row, "legacy_key display_order"); err != nil {
			return result, errors.New("resolution_invalid")
		}
	}
	if json.Unmarshal(raw, &result) != nil || result.Version != 1 || !result.DatesVerified ||
		!result.ConfigurationVerified ||
		!result.AdminGrantsVerified {
		return result, errors.New("resolution_attestation_required")
	}
	keys, positions := map[string]bool{}, map[int32]bool{}
	for _, row := range result.Events {
		if !digestPattern.MatchString(row.LegacyKey) || row.DisplayOrder == nil || *row.DisplayOrder < 0 {
			return result, errors.New("resolution_invalid")
		}
		if keys[row.LegacyKey] || positions[*row.DisplayOrder] {
			return result, errors.New("resolution_duplicate")
		}
		keys[row.LegacyKey], positions[*row.DisplayOrder] = true, true
	}
	return result, nil
}
