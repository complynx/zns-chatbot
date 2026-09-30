package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

type MessageResolution struct {
	LegacyKey     string   `json:"legacy_key"`
	Owner         string   `json:"owner"`
	ResolvedAt    string   `json:"resolved_at"`
	Disposition   string   `json:"disposition"`
	Provenance    string   `json:"provenance"`
	ArchiveFields []string `json:"archive_fields"`
}
type MessageResolutions struct {
	Version           int                 `json:"version"`
	PlanSHA256        string              `json:"plan_sha256"`
	ManifestSHA256    string              `json:"manifest_sha256"`
	BotID             int64               `json:"bot_id"`
	Collection        string              `json:"collection"`
	NamespaceAttested bool                `json:"namespace_attested"`
	OwnershipAttested bool                `json:"ownership_attested"`
	WritersStopped    bool                `json:"writers_stopped"`
	SourceClock       string              `json:"source_clock"`
	ClockAttested     bool                `json:"clock_attested"`
	Retention         string              `json:"retention,omitempty"`
	RetainFrom        string              `json:"retain_from,omitempty"`
	RetainUntil       string              `json:"retain_until,omitempty"`
	Messages          []MessageResolution `json:"messages"`
}
type preparedMessage struct {
	record   MessagePlanRecord
	decision MessageResolution
	instant  time.Time
	identity string
}
type preparedMessages struct {
	owners                   []string
	plan                     MessagePlan
	planHash, resolutionHash string
	rows                     []preparedMessage
}
type MessageValidationSummary struct {
	PlanSHA256       string `json:"plan_sha256"`
	ResolutionSHA256 string `json:"resolution_sha256"`
	Records          int    `json:"records"`
	Retained         int    `json:"retained"`
	Omitted          int    `json:"omitted"`
	Excluded         int    `json:"excluded"`
}

func ValidateMessages(stage, planPath, resolutionsPath string, limits Limits) (MessageValidationSummary, error) {
	prepared, err := prepareMessages(stage, planPath, resolutionsPath, limits)
	if err != nil {
		return MessageValidationSummary{}, err
	}
	return messageSummary(prepared), nil
}
func messageSummary(p preparedMessages) MessageValidationSummary {
	s := MessageValidationSummary{PlanSHA256: p.planHash, ResolutionSHA256: p.resolutionHash, Records: len(p.rows)}
	for _, row := range p.rows {
		switch row.decision.Disposition {
		case messageRetained:
			s.Retained++
		case messageOmitted:
			s.Omitted++
		default:
			s.Excluded++
		}
	}
	return s
}
func prepareMessages(stage, planPath, resolutionsPath string, limits Limits) (preparedMessages, error) {
	var p preparedMessages
	plan, generated, err := preparedMessagePlan(stage, limits)
	if err != nil {
		return p, err
	}
	original, err := readApplyFile(planPath, maxUserPlanBytes)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(original, generated) {
		return p, errors.New("apply_plan_mismatch")
	}
	raw, err := readApplyFile(resolutionsPath, maxUserResolutionBytes)
	if err != nil {
		return p, err
	}
	resolved, err := decodeMessageResolutions(raw)
	if err != nil {
		return p, err
	}
	p = preparedMessages{plan: plan, planHash: hashBytes(original), resolutionHash: hashBytes(raw)}
	if resolved.PlanSHA256 != p.planHash || resolved.ManifestSHA256 != plan.ManifestSHA256 ||
		resolved.BotID != plan.BotID ||
		resolved.Collection != plan.Collection {
		return p, errors.New("message_binding_mismatch")
	}
	if len(resolved.Messages) != len(plan.Messages) {
		return p, errors.New("message_resolution_set_mismatch")
	}
	decisions := map[string]MessageResolution{}
	for _, r := range resolved.Messages {
		if _, ok := decisions[r.LegacyKey]; ok {
			return p, errors.New("resolution_duplicate")
		}
		decisions[r.LegacyKey] = r
	}
	owners := map[int64]string{}
	for _, row := range plan.Messages {
		decision, present := decisions[row.Legacy.Key]
		if !present {
			return p, errors.New("message_resolution_set_mismatch")
		}
		prepared, resolveErr := resolveMessage(row, decision)
		if resolveErr != nil {
			return p, resolveErr
		}
		if previous, known := owners[row.Candidate.TelegramID]; known && previous != decision.Owner {
			return p, errors.New("message_owner_ambiguous")
		}
		owners[row.Candidate.TelegramID] = decision.Owner
		p.rows = append(p.rows, prepared)
	}
	p.owners = messageOwners(owners)
	slices.SortFunc(p.rows, compareMessageOrder)
	return p, nil
}
func decodeMessageResolutions(raw []byte) (MessageResolutions, error) {
	var r MessageResolutions
	if validJSON(raw) != nil {
		return r, errors.New("resolution_invalid")
	}
	fields, err := objectFields(
		raw,
		"version plan_sha256 manifest_sha256 bot_id collection namespace_attested ownership_attested writers_stopped source_clock clock_attested retention retain_from retain_until messages",
	)
	if err != nil {
		return r, errors.New("resolution_invalid")
	}
	var rows []json.RawMessage
	if json.Unmarshal(fields["messages"], &rows) != nil {
		return r, errors.New("resolution_invalid")
	}
	for _, row := range rows {
		if _, err = objectFields(
			row,
			"legacy_key owner resolved_at disposition provenance archive_fields",
		); err != nil {
			return r, errors.New("resolution_invalid")
		}
	}
	if json.Unmarshal(raw, &r) != nil || r.Version != 1 || !r.NamespaceAttested || !r.OwnershipAttested ||
		!r.WritersStopped ||
		!r.ClockAttested ||
		r.SourceClock != "per_record_reviewed" ||
		r.Collection == "" {
		return r, errors.New("message_attestation_required")
	}
	if err = validateMessageRetention(r, fields); err != nil {
		return r, err
	}
	for _, row := range r.Messages {
		if err = validateMessageDecision(row); err != nil {
			return r, err
		}
	}
	return r, nil
}

// Full-period retention permits reviewed privacy omissions, never date cutoffs.
func validateMessageRetention(r MessageResolutions, fields map[string]json.RawMessage) error {
	if r.Retention != "all" {
		return errors.New("message_retention_invalid")
	}
	_, from := fields["retain_from"]
	_, until := fields["retain_until"]
	if from || until {
		return errors.New("message_retention_conflict")
	}
	for _, row := range r.Messages {
		if row.Provenance == messageOutsideWindow {
			return errors.New("message_temporal_omission_invalid")
		}
	}
	return nil
}

func resolveMessage(row MessagePlanRecord, r MessageResolution) (preparedMessage, error) {
	var p preparedMessage
	if row.Candidate == nil || !slices.Equal(row.Blockers, []string{"message_review_required"}) {
		return p, errors.New("apply_record_blocked")
	}
	if !slices.Equal(r.ArchiveFields, row.Candidate.ArchiveFields) {
		return p, errors.New("message_fields_unresolved")
	}
	instant, _ := strictEventInstant(r.ResolvedAt)
	if r.Disposition == messageExcluded {
		if r.Provenance != messageOwnerExcluded {
			return p, errors.New("message_exclusion_invalid")
		}
		if r.Owner != "" {
			return p, errors.New("message_excluded_owner_invalid")
		}
	} else if r.Owner == "" {
		return p, errors.New("message_owner_invalid")
	}
	identity, _ := recordID(row.Legacy.RecordID)
	return preparedMessage{record: row, decision: r, instant: instant.UTC(), identity: identity}, nil
}
func validateMessageDecision(row MessageResolution) error {
	_, err := strictEventInstant(row.ResolvedAt)
	if err != nil || !digestPattern.MatchString(row.LegacyKey) ||
		(row.Owner != "" && !tokenPattern.MatchString(row.Owner)) {
		return errors.New("resolution_invalid")
	}
	var allowed bool
	switch row.Disposition {
	case messageRetained:
		allowed = row.Provenance == "ordinary_reviewed"
	case messageOmitted:
		allowed = slices.Contains([]string{"private", "sensitive", "expired", "unresolved_omitted"}, row.Provenance)
	case messageExcluded:
		allowed = row.Provenance == messageOwnerExcluded
	default:
		return errors.New("message_disposition_invalid")
	}
	if !allowed {
		return errors.New("message_provenance_required")
	}
	return nil
}

func compareMessageOrder(a, b preparedMessage) int {
	if n := a.instant.Compare(b.instant); n != 0 {
		return n
	}
	return strings.Compare(a.identity, b.identity)
}
func messageOwners(owners map[int64]string) []string {
	result := make([]string, 0, len(owners))
	for _, owner := range owners {
		if owner != "" {
			result = append(result, owner)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
