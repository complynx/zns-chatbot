package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
)

type MassagePlan struct {
	Version        int                 `json:"version"`
	ManifestSHA256 string              `json:"manifest_sha256"`
	BotID          int64               `json:"bot_id"`
	CapturedAt     string              `json:"captured_at"`
	Records        []MassagePlanRecord `json:"records"`
	Dependencies   []OrderDependency   `json:"dependencies"`
}
type MassagePlanRecord struct {
	Legacy        UserLegacyReference   `json:"legacy"`
	Record        json.RawMessage       `json:"record"`
	Source        string                `json:"source"`
	Excluded      bool                  `json:"excluded"`
	Blockers      []string              `json:"blockers"`
	Booking       *MassageCandidate     `json:"booking,omitempty"`
	Specialist    *MassageSpecialist    `json:"specialist,omitempty"`
	Configuration *MassageConfiguration `json:"configuration,omitempty"`
}
type MassagePlanSummary struct {
	ArtifactSHA256 string `json:"artifact_sha256"`
	Records        int    `json:"records"`
	Blocked        int    `json:"blocked"`
	Reused         bool   `json:"reused"`
}

func PlanMassage(stage, target string, limits Limits) (MassagePlanSummary, error) {
	p, raw, err := preparedMassagePlan(stage, limits)
	if err != nil {
		return MassagePlanSummary{}, err
	}
	result, err := writePrivatePlan(stage, target, func(w io.Writer) error { _, e := w.Write(raw); return e })
	summary := MassagePlanSummary{ArtifactSHA256: result.ArtifactSHA256, Records: len(p.Records), Reused: result.Reused}
	for _, row := range p.Records {
		if len(row.Blockers) > 0 {
			summary.Blocked++
		}
	}
	return summary, err
}
func preparedMassagePlan(stage string, limits Limits) (MassagePlan, []byte, error) {
	p := MassagePlan{Version: 1, Records: []MassagePlanRecord{}, Dependencies: []OrderDependency{}}
	if err := limits.validate(); err != nil {
		return p, nil, err
	}
	root, report, err := verifiedUsersStage(stage, limits)
	if err != nil {
		return p, nil, err
	}
	defer func() { _ = root.Close() }()
	manifest, err := readVerifiedUsersManifest(root, report, limits)
	if err != nil {
		return p, nil, err
	}
	p.BotID, p.ManifestSHA256, p.CapturedAt = manifest.BotID, report.ManifestSHA256, manifest.CapturedAt
	if p.BotID >= maxTelegramID {
		return p, nil, errors.New("bot_id_outside_target_range")
	}
	if err = p.readSources(root, manifest, limits); err != nil {
		return p, nil, err
	}
	var out bytes.Buffer
	if json.NewEncoder(&planBudgetWriter{target: &out, remaining: maxUserPlanBytes}).Encode(p) != nil {
		return p, nil, errors.New("plan_output_limit")
	}
	return p, out.Bytes(), nil
}
func massagePlanRow(row OrderPlanRecord, bot int64) (MassagePlanRecord, bool, error) {
	r := MassagePlanRecord{Legacy: row.Legacy, Record: row.Record, Source: row.Source, Blockers: []string{}}
	var f map[string]json.RawMessage
	if json.Unmarshal(row.Record, &f) != nil {
		return r, false, errors.New("massage_record_invalid")
	}
	var err error
	switch row.Source {
	case usersSource:
		if _, present := f[massageSpecialistField]; !present {
			return r, false, nil
		}
		id, valid := telegramNumber(f["bot_id"])
		switch {
		case !valid:
			err = errors.New("massage_bot_scope_invalid")
		case id != bot:
			r.Excluded = true
		default:
			r.Specialist, err = convertMassageSpecialist(f)
		}
	case orderConfigurationSource:
		var kind string
		_ = json.Unmarshal(f["kind"], &kind)
		if kind != "legacy_massage" {
			return r, false, nil
		}
		r.Configuration, err = convertMassageConfiguration(row.Record)
	case massageSource:
		if value, present := f["bot_id"]; present {
			id, valid := telegramNumber(value)
			if !valid {
				err = errors.New("massage_bot_scope_invalid")
			} else if id != bot {
				r.Excluded = true
			}
		}
		if err == nil && !r.Excluded {
			r.Booking, err = convertMassage(row.Record, bot)
		}
	}
	if err != nil {
		r.Blockers = append(r.Blockers, err.Error())
	}
	return r, true, nil
}

func (p *MassagePlan) readSources(root *os.Root, manifest Manifest, limits Limits) error {
	collections := map[string]string{}
	for _, source := range manifest.Coverage {
		if source.Status == sourceIncluded {
			collections[source.Domain] = source.Name
		}
	}
	if collections[massageSource] == "" || collections[usersSource] == "" || collections[eventsSource] == "" ||
		collections[orderConfigurationSource] == "" {
		return errors.New("massage_sources_required")
	}
	rows := OrderPlan{BotID: p.BotID}
	budget := &planBudgetWriter{target: io.Discard, remaining: maxUserPlanBytes}
	for _, file := range manifest.Files {
		if err := readMassageSourceFile(root, file, manifest, limits, collections, &rows, budget); err != nil {
			return err
		}
	}
	p.Dependencies = rows.Dependencies
	for _, row := range rows.Records {
		value, include, e := massagePlanRow(row, p.BotID)
		if e != nil {
			return e
		}
		if include {
			p.Records = append(p.Records, value)
		}
	}
	for _, proof := range manifest.Proofs {
		if proof.Source == massageSource {
			return errors.New("massage_proof_unmapped")
		}
	}
	return nil
}

func readMassageSourceFile(
	root *os.Root,
	file File,
	manifest Manifest,
	limits Limits,
	collections map[string]string,
	rows *OrderPlan,
	budget io.Writer,
) error {
	if file.Source == usersSource || file.Source == eventsSource {
		if err := readOrderDependencies(root, file, manifest, limits, rows, budget); err != nil {
			return err
		}
	}
	if file.Source != massageSource && file.Source != usersSource && file.Source != orderConfigurationSource {
		return nil
	}
	if file.Kind != recordsFileKind {
		if file.Source == orderConfigurationSource {
			return nil
		}
		return errors.New("massage_records_required")
	}
	if err := readOrderPlanFile(root, file, collections[file.Source], limits, rows, budget); err != nil {
		return err
	}
	return nil
}
