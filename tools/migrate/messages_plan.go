package migrate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type MessagePlan struct {
	Version        int                 `json:"version"`
	Collection     string              `json:"collection"`
	ManifestSHA256 string              `json:"manifest_sha256"`
	BotID          int64               `json:"bot_id"`
	Messages       []MessagePlanRecord `json:"messages"`
}
type MessagePlanSummary struct {
	ArtifactSHA256 string `json:"artifact_sha256"`
	Candidates     int    `json:"candidates"`
	Blocked        int    `json:"blocked"`
	Reused         bool   `json:"reused"`
}

func PlanMessages(stage, target string, limits Limits) (MessagePlanSummary, error) {
	plan, raw, err := preparedMessagePlan(stage, limits)
	if err != nil {
		return MessagePlanSummary{}, err
	}
	artifact, err := writePrivatePlan(
		stage,
		target,
		func(w io.Writer) error { _, writeErr := w.Write(raw); return writeErr },
	)
	summary := MessagePlanSummary{ArtifactSHA256: artifact.ArtifactSHA256, Reused: artifact.Reused}
	for _, row := range plan.Messages {
		if row.Candidate != nil {
			summary.Candidates++
		}
		if len(row.Blockers) > 0 {
			summary.Blocked++
		}
	}
	return summary, err
}
func preparedMessagePlan(stage string, limits Limits) (MessagePlan, []byte, error) {
	plan := MessagePlan{Version: 1, Messages: []MessagePlanRecord{}}
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
	for _, source := range manifest.Coverage {
		if source.Domain == messagesSource && source.Status == sourceIncluded {
			plan.Collection = source.Name
		}
	}
	if plan.Collection == "" {
		return plan, nil, errors.New("messages_source_required")
	}
	sourceRows := OrderPlan{BotID: plan.BotID}
	budget := &planBudgetWriter{target: io.Discard, remaining: maxUserPlanBytes}
	for _, file := range manifest.Files {
		if file.Source != messagesSource {
			continue
		}
		if file.Kind != recordsFileKind {
			return plan, nil, errors.New("messages_source_requires_records")
		}
		// Reuse the domain reader's strict parsing, canonical source reference,
		// line bounds and exact source-byte checksum verification.
		if err = readOrderPlanFile(root, file, plan.Collection, limits, &sourceRows, budget); err != nil {
			return plan, nil, err
		}
	}
	for _, source := range sourceRows.Records {
		var fields map[string]json.RawMessage
		if json.Unmarshal(source.Record, &fields) != nil {
			return plan, nil, errors.New("message_record_invalid")
		}
		row := convertMessage(fields)
		row.Legacy = source.Legacy
		plan.Messages = append(plan.Messages, row)
	}
	var output bytes.Buffer
	if json.NewEncoder(&planBudgetWriter{target: &output, remaining: maxUserPlanBytes}).Encode(plan) != nil {
		return plan, nil, errors.New("plan_output_limit")
	}
	return plan, output.Bytes(), nil
}
