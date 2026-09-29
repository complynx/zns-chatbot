package migrate_test

import (
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFoodRejectsChangedImportedEventBeforeApplyAndOnReplay(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, change string }{
		{"finish", `UPDATE core.pass_events SET finishes_at=finishes_at+interval '1 day' WHERE id='event_one'`},
		{"catalog_order", `UPDATE core.pass_events SET display_order=display_order+1 WHERE id='event_one'`},
		{"import_receipt", `UPDATE migrate_import.event_receipts SET plan_sha256=repeat('0',64)`},
		{"receipt_resolution", `UPDATE migrate_import.event_receipts SET resolution_sha256=repeat('1',64)`},
		{"receipt_admin", `UPDATE migrate_import.event_receipts SET admin_sha256=repeat('1',64)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for name, applied := range map[string]bool{"first_apply": false, "replay": true} {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					assertFoodRejectsChangedEvent(t, tc.change, applied)
				})
			}
		})
	}
}

func assertFoodRejectsChangedEvent(t *testing.T, change string, applied bool) {
	t.Helper()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := foodImportInputs(t)
	applyOrderDependencies(t, dsn, stage)
	if applied {
		_, err := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
		require.NoError(t, err)
	}
	_, err := db.Exec(t.Context(), change)
	require.NoError(t, err)
	for _, verify := range []bool{false, true} {
		if verify && !applied {
			continue
		}
		run := migrate.ApplyFood
		if verify {
			run = migrate.ReconcileFood
		}
		result, applyErr := run(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
		require.EqualError(t, applyErr, "food_event_dependency_changed")
		assert.False(t, result.Reconciled)
	}
	if !applied {
		var count int
		require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.food_events`).Scan(&count))
		assert.Zero(t, count)
	}
}

func TestFoodRequiresValidatedBaselineForLegacyEventReceipt(t *testing.T) {
	t.Parallel()
	for _, column := range []string{"event_snapshot", "receipt_snapshot"} {
		t.Run(column, func(t *testing.T) {
			t.Parallel()
			assertFoodLegacyReceiptUpgrade(t, column)
		})
	}
}

func assertFoodLegacyReceiptUpgrade(t *testing.T, column string) {
	t.Helper()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := foodImportInputs(t)
	applyOrderDependencies(t, dsn, stage)
	_, err := db.Exec(t.Context(), `ALTER TABLE migrate_import.event_receipts DROP COLUMN `+column)
	require.NoError(t, err)
	_, err = migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "food_event_receipt_reconcile_required")
	applyOrderDependencies(t, dsn, stage)
	result, err := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, result.Reconciled)
	_, err = db.Exec(t.Context(), `UPDATE migrate_import.event_receipts SET resolution_sha256=repeat('1',64)`)
	require.NoError(t, err)
	_, err = migrate.ReconcileFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "food_event_dependency_changed")
}

func TestFoodReceiptSurvivesValidatedEventAnchorUpgrade(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := foodImportInputs(t)
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var original string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT snapshot::text FROM migrate_import.food_receipts`).Scan(&original),
	)
	assert.NotContains(
		t,
		original,
		"receipt_snapshot",
		"legacy food receipt shape retains only original event receipt fields",
	)
	_, err = db.Exec(t.Context(), `ALTER TABLE migrate_import.event_receipts DROP COLUMN receipt_snapshot`)
	require.NoError(t, err)
	_, err = migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "food_event_receipt_reconcile_required")
	applyOrderDependencies(t, dsn, stage)
	result, err := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, result.Reused)
	assert.True(t, result.Reconciled)
	_, err = migrate.ReconcileFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var after string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT snapshot::text FROM migrate_import.food_receipts`).Scan(&after))
	assert.Equal(t, original, after, "food replay must not refresh its existing dependency baseline")
}
