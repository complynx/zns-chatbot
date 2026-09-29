package migrate_test

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const syntheticEvent = `{"_id":"event-one","key":"event_one","finish_date":{"$date":"2026-12-01T03:00:00+03:00"},"title_long":{"en":"Synthetic event","ru":"Тестовое событие"},"title_short":{"en":"Synthetic event","ru":"Тестовое событие"},"require_passport":true,"pass_assignment_rule":"paired","disable_max_concurrent_assignments":true,"pass_types":[{"amount":10,"price":120,"start":{"$date":"2026-09-01T00:00:00Z"},"promo":true},{"amount":20,"price":150,"start":"2026-10-01T00:00:00Z","blocked_by_date":true}]} `

func eventInputs(t *testing.T, records ...string) (string, string, string, migrate.EventPlan) {
	t.Helper()
	directory, manifest := snapshot(t)
	manifest.Files = nil
	for i := range manifest.Coverage {
		manifest.Coverage[i].Status = "absent"
		manifest.Coverage[i].Reason = "Synthetic fixture has no records"
	}
	require.NoError(t, os.Remove(filepath.Join(directory, "users.jsonl")))
	addFile(
		t,
		directory,
		&manifest,
		"events.jsonl",
		"events",
		"records",
		[]byte(strings.Join(records, "\n")+"\n"),
		int64(len(records)),
	)
	writeManifest(t, directory, manifest)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(directory, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	planPath := filepath.Join(t.TempDir(), "events.json")
	summary, err := migrate.PlanEvents(stage, planPath, migrate.DefaultLimits())
	require.NoError(t, err)
	var plan migrate.EventPlan
	raw, err := os.ReadFile(planPath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &plan))
	resolutions := migrate.EventResolutions{
		Version:               1,
		PlanSHA256:            summary.ArtifactSHA256,
		DatesVerified:         true,
		ConfigurationVerified: true,
		AdminGrantsVerified:   true,
	}
	for i, event := range plan.Events {
		position := int32(i)
		resolutions.Events = append(
			resolutions.Events,
			migrate.EventResolution{LegacyKey: event.Legacy.Key, DisplayOrder: &position},
		)
	}
	raw, err = json.Marshal(resolutions)
	require.NoError(t, err)
	links := filepath.Join(t.TempDir(), "resolutions.json")
	require.NoError(t, os.WriteFile(links, raw, 0o600))
	return stage, planPath, links, plan
}

func TestEventPlanPreservesCatalogAndImmutableArtifact(t *testing.T) {
	t.Parallel()
	stage, path, _, plan := eventInputs(t, syntheticEvent)
	require.Len(t, plan.Events, 1)
	row := plan.Events[0]
	require.Equal(t, []string{"event_policy_attestation_required"}, row.Blockers)
	require.NotNil(t, row.Candidate)
	assert.Equal(t, "2026-12-01T00:00:00Z", row.Candidate.FinishesAt.Format(time.RFC3339))
	assert.Equal(t, "Тестовое событие", row.Candidate.Titles["ru"])
	assert.True(t, row.Candidate.Tiers[0].Promo)
	assert.True(t, row.Candidate.Tiers[1].BlockedByDate)
	summary, err := migrate.PlanEvents(stage, path, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, summary.Reused)
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
	_, err = migrate.PlanEvents(stage, path, migrate.DefaultLimits())
	require.EqualError(t, err, "plan_replay_mismatch")
}

func TestEventPlanBlocksUnmappedDataBeforeDatabase(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, replacement string }{
		{"unknown", `"unknown_active":true`},
		{"capacity", `"amount_cap_per_role":null`},
		{"thread", `"thread_channel":null`},
		{"short title", `"title_short":42`},
		{"naive datetime", `"finish_date":"2026-12-01T00:00:00"`},
		{"invalid finish", `"finish_date":false`},
		{"fallback price", `"price":"120"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var record map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(syntheticEvent), &record))
			var replacement map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte("{"+tc.replacement+"}"), &replacement))
			maps.Copy(record, replacement)
			raw, err := json.Marshal(record)
			require.NoError(t, err)
			stage, path, links, plan := eventInputs(t, string(raw))
			assert.Nil(t, plan.Events[0].Candidate)
			_, err = migrate.ApplyEvents(t.Context(), "not-a-dsn", stage, path, links, migrate.DefaultLimits())
			require.EqualError(t, err, "apply_record_blocked")
		})
	}
}

func TestEventApplyPreservesCatalogReplayAndDrift(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, path, links, _ := eventInputs(t, syntheticEvent)
	summary, err := migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Applied)
	assert.True(t, summary.Reconciled)
	summary, err = migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Reused)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET price=777 WHERE position=1`)
	require.NoError(t, err)
	summary, err = migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
	assert.False(t, summary.Reconciled)
	var price int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT price FROM core.pass_event_tiers WHERE position=1`).Scan(&price),
	)
	assert.Equal(t, 777, price)
}

func TestEventApplyAdminDependencyRollbackAndResume(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	second := strings.ReplaceAll(strings.ReplaceAll(syntheticEvent, "event-one", "event-two"), "event_one", "event_two")
	second = strings.Replace(second, `"key":`, `"payment_admin":101,"hidden_payment_admins":[102],"key":`, 1)
	stage, path, links, _ := eventInputs(t, syntheticEvent, second)
	summary, err := migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_admin_identity_unresolved")
	assert.EqualValues(t, 1, summary.Applied)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_events`).Scan(&count))
	assert.Equal(t, 1, count)
	userStage, userPath, userLinks := applyInputs(
		t,
		`{"_id":"one","bot_id":77,"user_id":101,"print_name":"Admin One"}`,
		`{"_id":"two","bot_id":77,"user_id":102,"print_name":"Admin Two"}`,
	)
	_, err = migrate.ApplyUsers(t.Context(), dsn, userStage, userPath, userLinks, migrate.DefaultLimits())
	require.NoError(t, err)
	summary, err = migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, summary.Applied)
	assert.EqualValues(t, 1, summary.Reused)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_admins WHERE event_id='event_two' AND hidden`).
			Scan(&count),
	)
	assert.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_admins`).Scan(&count))
	assert.Zero(t, count)
	// An operator changing both the identity and grant must not make an old
	// receipt accept a different administrator owner.
	_, err = db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name) VALUES('replacement',999,'Replacement')`)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.users SET telegram_id=998 WHERE telegram_id=101`)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.users SET telegram_id=101 WHERE id='replacement'`)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.zitadel_identities(owner,issuer,subject) VALUES('replacement','https://synthetic.invalid','replacement')`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_payment_admins SET owner='replacement' WHERE event_id='event_two' AND NOT hidden`,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.telegram_identities SET owner='replacement' WHERE bot_id=77 AND telegram_id=101`,
	)
	require.NoError(t, err)
	_, err = migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}

func TestEventApplyRequiresExactAttestedInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*migrate.EventResolutions)
		want   string
	}{
		{"dates", func(r *migrate.EventResolutions) { r.DatesVerified = false }, "resolution_attestation_required"},
		{
			"configuration",
			func(r *migrate.EventResolutions) { r.ConfigurationVerified = false },
			"resolution_attestation_required",
		},
		{
			"grants",
			func(r *migrate.EventResolutions) { r.AdminGrantsVerified = false },
			"resolution_attestation_required",
		},
		{"order", func(r *migrate.EventResolutions) { r.Events[0].DisplayOrder = nil }, "resolution_invalid"},
		{
			"duplicate",
			func(r *migrate.EventResolutions) { r.Events = append(r.Events, r.Events[0]) },
			"resolution_duplicate",
		},
		{"missing", func(r *migrate.EventResolutions) { r.Events = nil }, "event_resolution_set_mismatch"},
		{
			"hash",
			func(r *migrate.EventResolutions) { r.PlanSHA256 = strings.Repeat("0", 64) },
			"resolution_plan_mismatch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage, path, links, _ := eventInputs(t, syntheticEvent)
			raw, err := os.ReadFile(links)
			require.NoError(t, err)
			var resolutions migrate.EventResolutions
			require.NoError(t, json.Unmarshal(raw, &resolutions))
			tc.change(&resolutions)
			raw, err = json.Marshal(resolutions)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(links, raw, 0o600))
			_, err = migrate.ApplyEvents(t.Context(), "not-a-dsn", stage, path, links, migrate.DefaultLimits())
			require.EqualError(t, err, tc.want)
		})
	}
}

func TestEventApplyConcurrentAndChangedReceipt(t *testing.T) {
	t.Parallel()
	dsn, _ := applyDatabase(t)
	stage, path, links, _ := eventInputs(t, syntheticEvent)
	type outcome struct {
		summary migrate.EventApplySummary
		err     error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			summary, err := migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
			results <- outcome{summary, err}
		}()
	}
	var applied, reused int64
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		assert.True(t, result.summary.Reconciled)
		applied += result.summary.Applied
		reused += result.summary.Reused
	}
	assert.EqualValues(t, 1, applied)
	assert.EqualValues(t, 1, reused)
	raw, err := os.ReadFile(links)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(links, append(raw, '\n'), 0o600))
	_, err = migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_receipt_conflict")
	require.NoError(t, os.WriteFile(path, []byte("{}"), 0o600))
	_, err = migrate.ApplyEvents(t.Context(), dsn, stage, path, links, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_plan_mismatch")
}

func TestEventPlanMaterializesPythonTitleFallback(t *testing.T) {
	t.Parallel()
	var record map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(syntheticEvent), &record))
	record["title_long"], record["title_short"] = json.RawMessage(
		`{"en-us":"Regional"}`,
	), json.RawMessage(
		`{"en-us":"Regional"}`,
	)
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	_, _, _, plan := eventInputs(t, string(raw))
	require.NotNil(t, plan.Events[0].Candidate)
	assert.Equal(t, "Regional", plan.Events[0].Candidate.Titles["ru"])
	record["title_long"], record["title_short"] = json.RawMessage(`"Shared title"`), json.RawMessage(`"Shared title"`)
	raw, err = json.Marshal(record)
	require.NoError(t, err)
	_, _, _, plan = eventInputs(t, string(raw))
	require.NotNil(t, plan.Events[0].Candidate)
	assert.Equal(t, "Shared title", plan.Events[0].Candidate.Titles["en"])
	assert.Equal(t, "Shared title", plan.Events[0].Candidate.Titles["ru"])
}
