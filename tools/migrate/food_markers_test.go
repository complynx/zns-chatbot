package migrate_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func TestFoodImportCompletesSeparatePassMarkers(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := foodImportInputs(t, func(t *testing.T, dir string, m *migrate.Manifest) {
		m.Files = slices.DeleteFunc(m.Files, func(file migrate.File) bool { return file.Path == "users.jsonl" })
		user := `{"_id":"user-a","bot_id":77,"user_id":101,"print_name":"Synthetic","event_one":{"state":"waitlist","role":"leader","proof_admin":101,"date_created":"2026-09-01T00:00:00Z","notified_food_first":true}}`
		addFile(t, dir, m, "users.jsonl", "users", "records", []byte(user+"\n"), 1)
		pass := `{"_id":"pass-one","bot_id":77,"user_id":101,"pass_key":"event_one","state":"waitlist","role":"leader","proof_admin":101,"date_created":"2026-09-01T00:00:00Z","notified_food_first":false,"notified_food_last":true}`
		addFile(t, dir, m, "passes.jsonl", "passes", "records", []byte(pass+"\n"), 1)
	})
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "food_pass_identity_unresolved")
	passPlan := filepath.Join(t.TempDir(), "passes.json")
	summary, err := migrate.PlanPasses(stage, passPlan, migrate.DefaultLimits())
	require.NoError(t, err)
	passRaw, readErr := os.ReadFile(passPlan)
	require.NoError(t, readErr)
	require.Zero(t, summary.Blocked, string(passRaw))
	resolved := migrate.PassResolutions{
		Version:                 1,
		PlanSHA256:              summary.ArtifactSHA256,
		DatesVerified:           true,
		BotNamespaceVerified:    true,
		WritersStopped:          true,
		HistoricalAnnouncements: "suppress_historical",
	}
	raw, err := json.Marshal(resolved)
	require.NoError(t, err)
	passResolution := filepath.Join(t.TempDir(), "passes-resolution.json")
	require.NoError(t, os.WriteFile(passResolution, raw, 0o600))
	_, err = migrate.ApplyPasses(t.Context(), dsn, stage, passPlan, passResolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var pending int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.legacy_user_deferred_domains WHERE domain='food' AND NOT completed)+(SELECT count(*) FROM core.legacy_pass_deferred_domains WHERE NOT completed)`).
			Scan(&pending),
	)
	require.Equal(t, 2, pending)
	_, err = migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.legacy_user_deferred_domains WHERE domain='food' AND NOT completed)+(SELECT count(*) FROM core.legacy_pass_deferred_domains WHERE NOT completed)`).
			Scan(&pending),
	)
	require.Zero(t, pending)
	var first, last int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FILTER(WHERE kind='no_order_first'),count(*) FILTER(WHERE kind='no_order_last') FROM core.food_notifications WHERE imported_sent`).
			Scan(&first, &last),
	)
	require.Zero(t, first)
	require.Equal(t, 1, last)
	_, err = migrate.ReconcileFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.legacy_pass_deferred_domains SET completed=false`)
	require.NoError(t, err)
	_, err = migrate.ReconcileFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}
