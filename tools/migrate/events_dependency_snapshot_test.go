package migrate_test

import (
	"encoding/json"
	"os"
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
	_, err = migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT event_snapshot IS NULL FROM migrate_import.event_receipts`).Scan(&matched),
	)
	assert.True(t, matched, "failed reconciliation must not bless modified live state")
	_, err = db.Exec(t.Context(), `UPDATE core.pass_events SET display_order=0`)
	require.NoError(t, err)
	result, err := migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.Reused)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT r.event_snapshot=to_jsonb(e) FROM migrate_import.event_receipts r JOIN core.legacy_event_references l USING(source_key) JOIN core.pass_events e ON e.id=l.event_id`).
			Scan(&matched),
	)
	assert.True(t, matched)
}

func TestEventReceiptAnchorUpgradeRequiresOriginalReceipt(t *testing.T) {
	t.Parallel()
	for _, column := range []string{"plan_sha256", "resolution_sha256", "admin_sha256"} {
		t.Run(column, func(t *testing.T) {
			t.Parallel()
			assertEventReceiptAnchorUpgrade(t, column)
		})
	}
}

func assertEventReceiptAnchorUpgrade(t *testing.T, column string) {
	t.Helper()
	dsn, db := applyDatabase(t)
	stage, plan, resolutions, _ := eventInputs(t, syntheticEvent)
	_, err := migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	var original string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT `+column+` FROM migrate_import.event_receipts`).Scan(&original))
	_, err = db.Exec(t.Context(), `ALTER TABLE migrate_import.event_receipts DROP COLUMN receipt_snapshot`)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE migrate_import.event_receipts SET `+column+`=repeat('1',64)`)
	require.NoError(t, err)
	_, err = migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.Error(t, err)
	assert.Contains(t, []string{"apply_receipt_conflict", "apply_reconciliation_failed"}, err.Error())
	var absent bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT receipt_snapshot IS NULL FROM migrate_import.event_receipts`).Scan(&absent),
	)
	assert.True(t, absent, "failed original reconciliation must not establish an anchor")
	_, err = db.Exec(t.Context(), `UPDATE migrate_import.event_receipts SET `+column+`=$1`, original)
	require.NoError(t, err)
	result, err := migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.EqualValues(t, 1, result.Reused)
	var matched bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT receipt_snapshot=jsonb_build_object('source_key',source_key,'plan_sha256',plan_sha256,'resolution_sha256',resolution_sha256,'admin_sha256',admin_sha256) FROM migrate_import.event_receipts`).
			Scan(&matched),
	)
	assert.True(t, matched)
}

func TestEventReceiptAnchorUpgradeRejectsChangedResolutionInput(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolutions, _ := eventInputs(t, syntheticEvent)
	_, err := migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `ALTER TABLE migrate_import.event_receipts DROP COLUMN receipt_snapshot`)
	require.NoError(t, err)
	raw, err := os.ReadFile(resolutions)
	require.NoError(t, err)
	var changed migrate.EventResolutions
	require.NoError(t, json.Unmarshal(raw, &changed))
	*changed.Events[0].DisplayOrder++
	raw, err = json.Marshal(changed)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(resolutions, raw, 0o600))
	_, err = migrate.ApplyEvents(t.Context(), dsn, stage, plan, resolutions, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_receipt_conflict")
	var absent bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT receipt_snapshot IS NULL FROM migrate_import.event_receipts`).Scan(&absent),
	)
	assert.True(t, absent)
}
