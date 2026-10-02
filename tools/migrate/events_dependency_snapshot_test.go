package migrate_test

import (
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventDependencyBaselineRequiresOriginalReconciliation(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolutions, _ := eventInputs(t, syntheticEvent)
	_, err := migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	var matched bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT r.event_snapshot=to_jsonb(e) FROM migrate_import.event_receipts r JOIN core.legacy_event_references l USING(source_key) JOIN core.pass_events e ON e.id=l.event_id`).
			Scan(&matched),
	)
	assert.True(t, matched)
	_, err = db.Exec(
		t.Context(),
		`UPDATE migrate_import.event_receipts SET event_snapshot=NULL; UPDATE core.pass_events SET display_order=display_order+1`,
	)
	require.NoError(t, err)
	_, err = migrate.ReconcileEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT event_snapshot IS NULL FROM migrate_import.event_receipts`).Scan(&matched),
	)
	assert.True(t, matched, "failed reconciliation must not bless modified live state")
}
