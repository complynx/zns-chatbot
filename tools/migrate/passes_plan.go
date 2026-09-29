package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
)

type PassPlan struct {
	Version        int               `json:"version"`
	ManifestSHA256 string            `json:"manifest_sha256"`
	BotID          int64             `json:"bot_id"`
	Records        []PassPlanRecord  `json:"records"`
	Dependencies   []OrderDependency `json:"dependencies"`
	Proofs         []Proof           `json:"proofs"`
	Files          []File            `json:"files"`
}
type PassPlanRecord struct {
	Source         string              `json:"source"`
	Legacy         UserLegacyReference `json:"legacy"`
	Record         json.RawMessage     `json:"record"`
	Field          string              `json:"field,omitempty"`
	Candidate      *PassCandidate      `json:"candidate,omitempty"`
	Preferences    map[string]int64    `json:"preferences,omitempty"`
	PassportMarker bool                `json:"passport_marker,omitempty"`
	TelegramID     int64               `json:"telegram_id,omitempty"`
	Shadowed       bool                `json:"shadowed,omitempty"`
	Excluded       bool                `json:"excluded,omitempty"`
	Blockers       []string            `json:"blockers"`
}
type PassPlanSummary struct {
	ArtifactSHA256 string `json:"artifact_sha256"`
	Records        int    `json:"records"`
	Blocked        int    `json:"blocked"`
	Reused         bool   `json:"reused"`
}

func PlanPasses(stage, target string, limits Limits) (PassPlanSummary, error) {
	plan, raw, err := preparedPassPlan(stage, limits)
	if err != nil {
		return PassPlanSummary{}, err
	}
	artifact, err := writePrivatePlan(
		stage,
		target,
		func(w io.Writer) error { _, writeErr := w.Write(raw); return writeErr },
	)
	summary := PassPlanSummary{
		ArtifactSHA256: artifact.ArtifactSHA256,
		Records:        len(plan.Records),
		Reused:         artifact.Reused,
	}
	for _, r := range plan.Records {
		if len(r.Blockers) > 0 {
			summary.Blocked++
		}
	}
	return summary, err
}
func preparedPassPlan(stage string, limits Limits) (PassPlan, []byte, error) {
	plan := PassPlan{
		Version:      passPlanVersion,
		Records:      []PassPlanRecord{},
		Dependencies: []OrderDependency{},
		Proofs:       []Proof{},
	}
	if err := limits.validate(); err != nil {
		return plan, nil, err
	}
	root, report, err := verifiedUsersStage(stage, limits)
	if err != nil {
		return plan, nil, err
	}
	defer func() { _ = root.Close() }()
	manifest, err := readVerifiedUsersManifest(root, report, limits)
	if err != nil {
		return plan, nil, err
	}
	if manifest.BotID >= maxTelegramID {
		return plan, nil, errors.New("bot_id_outside_target_range")
	}
	plan.BotID, plan.ManifestSHA256 = manifest.BotID, report.ManifestSHA256
	rows, err := passSourceRows(root, manifest, limits)
	if err != nil {
		return plan, nil, err
	}
	if err = plan.buildRecords(rows); err != nil {
		return plan, nil, err
	}
	for _, proof := range manifest.Proofs {
		if proof.Source == passesSource || proof.Source == usersSource {
			plan.Proofs = append(plan.Proofs, proof)
		}
	}
	plan.Files = manifest.Files
	var out bytes.Buffer
	if json.NewEncoder(&planBudgetWriter{target: &out, remaining: maxUserPlanBytes}).Encode(plan) != nil {
		return plan, nil, errors.New("plan_output_limit")
	}
	return plan, out.Bytes(), nil
}
func passSourceRows(root *os.Root, manifest Manifest, limits Limits) ([]OrderPlanRecord, error) {
	collections := map[string]string{}
	for _, source := range manifest.Coverage {
		if source.Status == sourceIncluded {
			collections[source.Domain] = source.Name
		}
	}
	if collections[usersSource] == "" {
		return nil, errors.New("pass_dependencies_required")
	}
	rows := OrderPlan{BotID: manifest.BotID}
	budget := &planBudgetWriter{target: io.Discard, remaining: maxUserPlanBytes}
	for _, file := range manifest.Files {
		if file.Source != passesSource && file.Source != usersSource && file.Source != eventsSource {
			continue
		}
		if file.Kind != recordsFileKind {
			return nil, errors.New("pass_records_required")
		}
		if err := readOrderPlanFile(root, file, collections[file.Source], limits, &rows, budget); err != nil {
			return nil, err
		}
	}
	return rows.Records, nil
}
func (p *PassPlan) buildRecords(rows []OrderPlanRecord) error {
	events := map[string]bool{}
	dedicated := map[string]bool{}
	for _, row := range rows {
		if row.Source == eventsSource {
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(row.Record, &fields)
			var key string
			if json.Unmarshal(fields["key"], &key) != nil || !tokenPattern.MatchString(key) {
				return errors.New("pass_event_dependency_invalid")
			}
			events[key] = true
			p.Dependencies = append(
				p.Dependencies,
				OrderDependency{Source: eventsSource, Legacy: row.Legacy, EventID: key},
			)
		}
	}
	for _, row := range rows {
		if row.Source == passesSource {
			if err := p.addDedicatedPass(row, dedicated); err != nil {
				return err
			}
		}
	}
	for _, row := range rows {
		if row.Source == usersSource {
			if err := p.addPassUser(row, events, dedicated); err != nil {
				return err
			}
		}
	}
	return nil
}
func (p *PassPlan) addDedicatedPass(row OrderPlanRecord, dedicated map[string]bool) error {
	parsed := PassPlanRecord{Source: row.Source, Legacy: row.Legacy, Record: row.Record, Blockers: []string{}}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Record, &fields)
	bot, ok := telegramNumber(fields["bot_id"])
	switch {
	case !ok:
		parsed.Blockers = append(parsed.Blockers, "pass_bot_invalid")
	case bot != p.BotID:
		parsed.Excluded = true
	default:
		candidate, err := convertPass(row.Record)
		parsed.Candidate = candidate
		if err != nil {
			parsed.Blockers = append(parsed.Blockers, err.Error())
		} else {
			identity := passIdentity(candidate.Event, candidate.TelegramID)
			if dedicated[identity] {
				return errors.New("pass_registration_duplicate")
			}
			dedicated[identity] = true
		}
	}
	p.Records = append(p.Records, parsed)
	return nil
}
func (p *PassPlan) addPassUser(row OrderPlanRecord, events, dedicated map[string]bool) error {
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(row.Record, &fields)
	bot, ok := telegramNumber(fields["bot_id"])
	if !ok {
		return errors.New("pass_dependency_invalid")
	}
	if bot != p.BotID {
		return nil
	}
	id, ok := telegramNumber(fields["user_id"])
	if !ok {
		return errors.New("pass_dependency_invalid")
	}
	p.Dependencies = append(p.Dependencies, OrderDependency{Source: usersSource, Legacy: row.Legacy, TelegramID: id})
	keys := make([]string, 0, len(events))
	for key := range events {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if _, exists := fields[key]; exists {
			p.Records = append(p.Records, embeddedPassRecord(row, fields, key, id, dedicated[passIdentity(key, id)]))
		}
	}
	if preference := passPreferenceRecord(row, fields, id, events); preference != nil {
		p.Records = append(p.Records, *preference)
	}
	return nil
}

func embeddedPassRecord(
	row OrderPlanRecord,
	fields map[string]json.RawMessage,
	key string,
	id int64,
	shadowed bool,
) PassPlanRecord {
	record := PassPlanRecord{
		Source:   usersSource,
		Legacy:   row.Legacy,
		Record:   row.Record,
		Field:    key,
		Shadowed: shadowed,
		Blockers: []string{},
	}
	record.Legacy.Key = hashBytes([]byte(row.Legacy.Key + ":pass:" + key))
	if shadowed {
		record.Candidate = &PassCandidate{Event: key, TelegramID: id}
		return record
	}
	var embedded map[string]json.RawMessage
	if json.Unmarshal(fields[key], &embedded) != nil || embedded == nil {
		record.Blockers = append(record.Blockers, "pass_embedded_invalid")
		return record
	}
	embedded["user_id"] = fields["user_id"]
	embedded["pass_key"], _ = json.Marshal(key)
	raw, _ := json.Marshal(embedded)
	candidate, err := convertPass(raw)
	record.Candidate = candidate
	if err != nil {
		record.Blockers = append(record.Blockers, err.Error())
	}
	return record
}

func passPreferenceRecord(
	row OrderPlanRecord,
	fields map[string]json.RawMessage,
	id int64,
	events map[string]bool,
) *PassPlanRecord {
	_, notified := fields["notified_passport_data_required"]
	_, passport := fields["passport_number"]
	raw, preferences := fields["proof_admins"]
	if !preferences && !notified && !passport {
		return nil
	}
	record := &PassPlanRecord{
		Source:         usersSource,
		Legacy:         row.Legacy,
		Record:         row.Record,
		TelegramID:     id,
		PassportMarker: notified || passport,
		Blockers:       []string{},
	}
	record.Legacy.Key = hashBytes([]byte(row.Legacy.Key + ":pass-preferences"))
	if !preferences {
		return record
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		record.Blockers = append(record.Blockers, "pass_preferences_invalid")
		return record
	}
	record.Preferences = map[string]int64{}
	for event, value := range values {
		admin, valid := telegramNumber(value)
		if !events[event] || !valid {
			record.Blockers = append(record.Blockers, "pass_preference_dependency_invalid")
			continue
		}
		record.Preferences[event] = admin
	}
	return record
}
func passProofID(bot int64, key string) string {
	return hashBytes([]byte("pass-proof:" + strconv.FormatInt(bot, 10) + ":" + key))
}
