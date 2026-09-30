package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

const syntheticMessage = `{"_id":"message-one","user_id":101,"role":"user","content":"ordinary synthetic text","date":{"$date":"2035-06-01T00:00:00Z"}}`

func messageInputs(
	t *testing.T,
	records ...string,
) (string, string, string, migrate.MessagePlan, migrate.MessageResolutions) {
	t.Helper()
	directory, manifest := snapshot(t)
	manifest.Files = nil
	for i := range manifest.Coverage {
		manifest.Coverage[i].Status = "absent"
		manifest.Coverage[i].Reason = "Synthetic fixture"
	}
	require.NoError(t, os.Remove(filepath.Join(directory, "users.jsonl")))
	addFile(
		t,
		directory,
		&manifest,
		"messages.jsonl",
		"messages",
		"records",
		[]byte(strings.Join(records, "\n")+"\n"),
		int64(len(records)),
	)
	writeManifest(t, directory, manifest)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(directory, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	planPath := filepath.Join(t.TempDir(), "messages.json")
	summary, err := migrate.PlanMessages(stage, planPath, migrate.DefaultLimits())
	require.NoError(t, err)
	raw, err := os.ReadFile(planPath)
	require.NoError(t, err)
	var plan migrate.MessagePlan
	require.NoError(t, json.Unmarshal(raw, &plan))
	r := migrate.MessageResolutions{
		Version:           1,
		PlanSHA256:        summary.ArtifactSHA256,
		ManifestSHA256:    plan.ManifestSHA256,
		BotID:             plan.BotID,
		Collection:        plan.Messages[0].Legacy.Collection,
		NamespaceAttested: true,
		OwnershipAttested: true,
		WritersStopped:    true,
		SourceClock:       "per_record_reviewed",
		ClockAttested:     true,
		Retention:         "all",
	}
	for _, row := range plan.Messages {
		r.Messages = append(
			r.Messages,
			migrate.MessageResolution{
				LegacyKey:     row.Legacy.Key,
				Owner:         "synthetic-owner",
				ResolvedAt:    "2035-06-01T00:00:00Z",
				Disposition:   "retained",
				Provenance:    "ordinary_reviewed",
				ArchiveFields: []string{},
			},
		)
	}
	path := filepath.Join(t.TempDir(), "resolutions.json")
	writeMessageResolutions(t, path, r)
	return stage, planPath, path, plan, r
}
func writeMessageResolutions(t *testing.T, path string, r migrate.MessageResolutions) {
	t.Helper()
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
}
func TestMessagesPlanAndValidation(t *testing.T) {
	t.Parallel()
	body := strings.Repeat("Ю界😀", 2000) + "TERMINAL_MARKER"
	record := strings.Replace(syntheticMessage, "ordinary synthetic text", body, 1)
	stage, path, resolutions, plan, _ := messageInputs(t, record)
	require.Equal(t, body, plan.Messages[0].Candidate.Content)
	summary, err := migrate.PlanMessages(stage, path, migrate.DefaultLimits())
	require.NoError(t, err)
	require.True(t, summary.Reused)
	checked, err := migrate.ValidateMessages(stage, path, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, 1, checked.Retained)
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0600))
	_, err = migrate.ValidateMessages(stage, path, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_plan_mismatch")
}
func TestMessagesResolutionRefusesUnsafeDecisions(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*migrate.MessageResolutions){
		"namespace":  func(r *migrate.MessageResolutions) { r.NamespaceAttested = false },
		"clock":      func(r *migrate.MessageResolutions) { r.ClockAttested = false },
		"retention":  func(r *migrate.MessageResolutions) { r.Retention = "" },
		"provenance": func(r *migrate.MessageResolutions) { r.Messages[0].Provenance = "unknown" },
		"owner":      func(r *migrate.MessageResolutions) { r.Messages[0].Owner = "" },
		"instant":    func(r *migrate.MessageResolutions) { r.Messages[0].ResolvedAt = "2035-06-01T00:00:00" },
		"window":     func(r *migrate.MessageResolutions) { r.Retention = "window" },
		"binding":    func(r *migrate.MessageResolutions) { r.BotID++ },
		"duplicate":  func(r *migrate.MessageResolutions) { r.Messages = append(r.Messages, r.Messages[0]) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stage, plan, resolutions, _, r := messageInputs(t, syntheticMessage)
			change(&r)
			writeMessageResolutions(t, resolutions, r)
			_, err := migrate.ValidateMessages(stage, plan, resolutions, migrate.DefaultLimits())
			require.Error(t, err)
		})
	}
}
func TestMessagesBlockedSource(t *testing.T) {
	t.Parallel()
	for name, record := range map[string]string{"role": strings.Replace(syntheticMessage, `"role":"user"`, `"role":"system"`, 1), "owner": strings.Replace(syntheticMessage, `"user_id":101`, `"user_id":null`, 1), "nul": strings.Replace(syntheticMessage, "ordinary synthetic text", `\u0000`, 1)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stage, plan, resolutions, _, _ := messageInputs(t, record)
			_, err := migrate.ValidateMessages(stage, plan, resolutions, migrate.DefaultLimits())
			require.EqualError(t, err, "apply_record_blocked")
		})
	}
}
func TestMessagesUnknownFieldsNeedArchiveDisposition(t *testing.T) {
	t.Parallel()
	record := strings.TrimSuffix(syntheticMessage, "}") + `,"tokens":123,"unrecognized":{"private":"ARCHIVE_CANARY"}}`
	stage, path, resolutions, plan, r := messageInputs(t, record)
	_, err := migrate.ValidateMessages(stage, path, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "message_fields_unresolved")
	r.Messages[0].ArchiveFields = plan.Messages[0].Candidate.ArchiveFields
	writeMessageResolutions(t, resolutions, r)
	_, err = migrate.ValidateMessages(stage, path, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "ARCHIVE_CANARY")
}
func TestMessagesReviewedOmissionsRejectTemporalExclusion(t *testing.T) {
	t.Parallel()
	stage, plan, resolutions, _, r := messageInputs(t, syntheticMessage)
	r.Messages[0].Disposition = "omitted"
	r.Messages[0].Provenance = "unresolved_omitted"
	writeMessageResolutions(t, resolutions, r)
	result, err := migrate.ValidateMessages(stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Equal(t, 1, result.Omitted)
	r.Messages[0].Disposition = "excluded"
	r.Messages[0].Provenance = "outside_window"
	r.Messages[0].ResolvedAt = "1970-01-01T00:00:00Z"
	writeMessageResolutions(t, resolutions, r)
	result, err = migrate.ValidateMessages(stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "message_temporal_omission_invalid")
	require.Zero(t, result.Excluded)
}
func TestMessagesStrictResolution(t *testing.T) {
	t.Parallel()
	for _, suffix := range []string{`,"Extra":true}`, `,"version":1}`, `,"Version":1}`} {
		stage, plan, resolutions, _, r := messageInputs(t, syntheticMessage)
		raw, err := json.Marshal(r)
		require.NoError(t, err)
		raw = append(raw[:len(raw)-1], suffix...)
		require.NoError(t, os.WriteFile(resolutions, raw, 0600))
		_, err = migrate.ValidateMessages(stage, plan, resolutions, migrate.DefaultLimits())
		require.Error(t, err)
	}
}
