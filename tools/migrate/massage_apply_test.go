package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func massageImportInputs(t *testing.T, edit func(string) string) (string, string, string) {
	t.Helper()
	dir, m := snapshot(t)
	m.Files = nil
	users := `{"_id":"u101","bot_id":77,"user_id":101,"print_name":"Synthetic","massage_specialist":{"about":"Bodywork","about_ru":"Массаж","notify_bookings":false,"notify_next":true,"table_not_required":true,"work_hours":[{"start":"2026-12-01T18:00:00+03:00","end":"2026-12-02T02:00:00+03:00"}]}}` + "\n" + `{"_id":"u202","bot_id":77,"user_id":202,"print_name":"Client"}` + "\n"
	addFile(t, dir, &m, "users.jsonl", "users", "records", []byte(users), 2)
	addFile(t, dir, &m, "events.jsonl", "events", "records", []byte(syntheticEvent+"\n"), 1)
	config := `{"_id":"massage-config","kind":"legacy_massage","event_key":"event_one","daily_limit":3,"prior_long_seconds":3600,"prior_short_seconds":600,"parties":[{"start":"2026-12-01T18:00:00+03:00","end":"2026-12-02T02:00:00+03:00","massage_tables":1,"is_open":false}]}`
	addFile(t, dir, &m, "configuration.jsonl", "configuration", "records", []byte(config+"\n"), 1)
	records := `{"_id":{"$oid":"000000000000000000000001"},"user_id":202,"pass_key":"event_one","length":2,"party":1,"slot":3,"specialist":101,"start":"2026-12-01T19:00:00+03:00","end":"2026-12-01T19:35:00+03:00","created_at":"2026-09-01T12:00:00+03:00","finalized_at":"2026-09-01T12:01:00+03:00","client_notified_prior_long":false,"specialist_notified":true,"notify":true}` + "\n" + `{"_id":{"$oid":"000000000000000000000002"},"user_id":202,"pass_key":"event_one","created_at":"2026-09-01T12:00:00+03:00","length":1,"party":1,"page":2,"specialists_choices":{"101":false},"choices":{"0":{"page":-1},"1":{"slot":5,"specialist":101},"2":{"specialist":101},"3":{"party":1}}}` + "\n"
	if edit != nil {
		records = edit(records)
	}
	addFile(t, dir, &m, "massage.jsonl", "massage", "records", []byte(records), 2)
	writeManifest(t, dir, m)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(dir, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	plan := filepath.Join(t.TempDir(), "massage.json")
	summary, err := migrate.PlanMassage(stage, plan, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Zero(t, summary.Blocked)
	r := migrate.MassageResolutions{
		Version:               1,
		PlanSHA256:            summary.ArtifactSHA256,
		DatesVerified:         true,
		ConfigurationVerified: true,
		BotNamespaceVerified:  true,
		WritersStopped:        true,
	}
	raw, err := json.Marshal(r)
	require.NoError(t, err)
	resolution := filepath.Join(t.TempDir(), "resolutions.json")
	require.NoError(t, os.WriteFile(resolution, raw, 0o600))
	return stage, plan, resolution
}
func TestMassageImportPreservesStateReplayAndDrift(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := massageImportInputs(t, nil)
	applyOrderDependencies(t, dsn, stage)
	summary, err := migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, summary.Reconciled)
	var owner, specialist string
	var price, count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT owner,specialist,price FROM core.massage_bookings`).
			Scan(&owner, &specialist, &price),
	)
	assert.Equal(t, "owner-202", owner)
	assert.Equal(t, "owner-101", specialist)
	assert.Equal(t, 57, price)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices WHERE sent_at IS NOT NULL`).Scan(&count),
	)
	assert.Equal(t, 2, count)
	var state map[string]any
	require.NoError(t, db.QueryRow(t.Context(), `SELECT state FROM core.legacy_massage_drafts`).Scan(&state))
	assert.Equal(t, "legacy-massage-party:event_one:1", state["party"])
	assert.InDelta(t, 2, state["page"], 0)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.legacy_user_deferred_domains WHERE domain='massage' AND completed`).
			Scan(&count),
	)
	assert.Equal(t, 1, count)
	summary, err = migrate.ReconcileMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, summary.Reused)
	_, err = db.Exec(t.Context(), `UPDATE core.legacy_massage_drafts SET version=version+1`)
	require.NoError(t, err)
	_, err = migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}
func TestMassageImportRollsBackUnresolvedDraft(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := massageImportInputs(t, func(s string) string {
		return strings.ReplaceAll(s, `"slot":5,"specialist":101`, `"slot":5,"specialist":303`)
	})
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "massage_draft_specialist_unresolved")
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_events`).Scan(&count))
	assert.Zero(t, count)
}

func TestMassageImportConcurrentReplayAndConflictResume(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := massageImportInputs(t, nil)
	applyOrderDependencies(t, dsn, stage)
	_, err := db.Exec(t.Context(), `INSERT INTO core.massage_events(id) VALUES('event_one')`)
	require.NoError(t, err)
	_, err = migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "massage_target_conflict")
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.legacy_massage_import_references`).Scan(&count),
	)
	assert.Zero(t, count)
	_, err = db.Exec(t.Context(), `DELETE FROM core.massage_events WHERE id='event_one'`)
	require.NoError(t, err)
	var summaries [2]migrate.MassageApplySummary
	var failures [2]error
	var workers sync.WaitGroup
	for i := range summaries {
		workers.Go(func() {
			summaries[i], failures[i] = migrate.ApplyMassage(
				t.Context(),
				dsn,
				stage,
				plan,
				resolution,
				migrate.DefaultLimits(),
			)
		})
	}
	workers.Wait()
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	assert.NotEqual(t, summaries[0].Reused, summaries[1].Reused)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_bookings`).Scan(&count))
	assert.Equal(t, 1, count)
}
