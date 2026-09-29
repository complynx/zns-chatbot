package migrate_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func changeOrderSource(t *testing.T, dir string, manifest *migrate.Manifest, path, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, path))
	require.NoError(t, err)
	changed := []byte(strings.Replace(string(raw), from, to, 1))
	require.NotEqual(t, string(raw), string(changed))
	require.NoError(t, os.WriteFile(filepath.Join(dir, path), changed, 0o600))
	hash := sha256.Sum256(changed)
	for i := range manifest.Files {
		if manifest.Files[i].Path == path {
			manifest.Files[i].Bytes = int64(len(changed))
			manifest.Files[i].SHA256 = hex.EncodeToString(hash[:])
		}
	}
}

func TestApplyOrdersRefusesUnresolvedSourceBeforeDatabase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, from, to, code string }{
		{"unknown field", "orders.jsonl", `"user_id":101`, `"user_id":101,"unknown_active":true`, "apply_record_blocked"},
		{"historical money inconsistency", "orders.jsonl", `"price":9.99`, `"price":9.98`, "apply_record_blocked"},
		{"attempt mismatch", "slots.jsonl", `"attempt-one"`, `"different-attempt"`, "order_reservation_attempt_unresolved"},
		{"empty active token", "orders.jsonl", `"payment_attempt_token":"attempt-one"`, `"payment_attempt_token":""`, "apply_record_blocked"},
		{"invalid cash origin", "orders.jsonl", `"proof_file":"cash"`, `"proof_file":false`, "apply_record_blocked"},
		{"metadata on actual proof", "orders.jsonl", `"proof_file":"synthetic-file"`, `"proof_file":"synthetic-file","_migration":{"payment_reservation_token":"legacy-validation:true"}`, "apply_record_blocked"},
		{"seat null", "slots.jsonl", `"seat":0`, `"seat":null`, "apply_record_blocked"},
		{"other bot", "users.jsonl", `"bot_id":77`, `"bot_id":88`, "order_owner_dependency_required"},
		{"unsupported exchange", "configuration.jsonl", `"byn_to_rub":30`, `"byn_to_rub":31`, "apply_record_blocked"},
		{"null legacy", "configuration.jsonl", `"price":65`, `"legacy":null,"price":65`, "apply_record_blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stage, plan, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, m *migrate.Manifest) {
				changeOrderSource(t, dir, m, tc.path, tc.from, tc.to)
			})
			_, err := migrate.ApplyOrders(
				t.Context(),
				"invalid database",
				stage,
				plan,
				resolution,
				migrate.DefaultLimits(),
			)
			require.EqualError(t, err, tc.code)
		})
	}
	stage, plan, resolution := orderImportInputs(
		t,
		false,
		func(_ *testing.T, _ string, m *migrate.Manifest) { m.Proofs[0].MessageID++ },
	)
	_, err := migrate.ApplyOrders(t.Context(), "invalid database", stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "order_proof_origin_mismatch")
}

func TestApplyOrdersRejectsTokenedReservationForUnselectedService(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := orderImportInputs(t, false, func(t *testing.T, dir string, m *migrate.Manifest) {
		changeOrderSource(t, dir, m, "orders.jsonl", `"extras":{"shuttle":65`, `"extras":{"preparty":65`)
	})
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "order_reservation_service_unselected")
	var events, orders, slots, proofs, refs int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
	(SELECT count(*) FROM core.order_events), (SELECT count(*) FROM core.orders),
	(SELECT count(*) FROM core.order_capacity_slots), (SELECT count(*) FROM core.order_proofs),
	(SELECT count(*) FROM core.legacy_order_import_references)`).Scan(&events, &orders, &slots, &proofs, &refs))
	assert.Zero(t, events)
	assert.Zero(t, orders)
	assert.Zero(t, slots)
	assert.Zero(t, proofs)
	assert.Zero(t, refs)
}

func TestReconcileOrdersDoesNotCreateRowsAndDetectsProofDrift(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := orderImportInputs(t, false)
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_receipt_unavailable")
	var exists bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT to_regclass('migrate_import.order_receipts') IS NOT NULL`).Scan(&exists),
	)
	assert.False(t, exists)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_events`).Scan(&count))
	assert.Zero(t, count)
	_, err = migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	summary, err := migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	assert.True(t, summary.Reconciled)
	assert.Zero(t, summary.Applied)
	_, err = db.Exec(t.Context(), `UPDATE core.order_proofs SET body='changed later'::bytea`)
	require.NoError(t, err)
	_, err = migrate.ReconcileOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
	var body []byte
	require.NoError(t, db.QueryRow(t.Context(), `SELECT body FROM core.order_proofs`).Scan(&body))
	assert.Equal(t, []byte("changed later"), body)
}

func TestApplyOrdersChangedPlanAndAttestationRefused(t *testing.T) {
	t.Parallel()
	stage, plan, resolution := orderImportInputs(t, false)
	raw, err := os.ReadFile(resolution)
	require.NoError(t, err)
	require.NoError(
		t,
		os.WriteFile(
			resolution,
			[]byte(strings.Replace(string(raw), `"writers_stopped":true`, `"writers_stopped":false`, 1)),
			0o600,
		),
	)
	_, err = migrate.ApplyOrders(t.Context(), "invalid database", stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "resolution_attestation_required")
	require.NoError(t, os.WriteFile(plan, []byte("{}"), 0o600))
	_, err = migrate.ApplyOrders(t.Context(), "invalid database", stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_plan_mismatch")
}

func TestApplyOrdersRollsBackCatalogProofAndReferencesOnOrderConflict(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := orderImportInputs(t, false)
	applyOrderDependencies(t, dsn, stage)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.order_events(id,deadline,menu,extras) VALUES('unrelated',now(),'{}','{}');
	INSERT INTO core.orders(id,event_id,owner,version,choice,state) VALUES('legacy-order:77:000000000000000000000001','unrelated','owner-101',1,'{}','unpaid')`,
	)
	require.NoError(t, err)
	summary, err := migrate.ApplyOrders(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_order_conflict")
	assert.Zero(t, summary.Applied)
	var events, proofs, refs int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.order_events WHERE id='event_one'),(SELECT count(*) FROM core.order_proofs),(SELECT count(*) FROM core.legacy_order_import_references)`).
			Scan(&events, &proofs, &refs),
	)
	assert.Zero(t, events)
	assert.Zero(t, proofs)
	assert.Zero(t, refs)
	var event string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT event_id FROM core.orders`).Scan(&event))
	assert.Equal(t, "unrelated", event)
}
