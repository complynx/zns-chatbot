package migrate_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
	"github.com/stretchr/testify/require"
)

func foodImportInputs(t *testing.T, edits ...func(*testing.T, string, *migrate.Manifest)) (string, string, string) {
	t.Helper()
	dir, m := snapshot(t)
	m.Files = nil
	addFile(
		t,
		dir,
		&m,
		"users.jsonl",
		"users",
		"records",
		[]byte(`{"_id":"user-a","bot_id":77,"user_id":101,"print_name":"Synthetic"}`+"\n"),
		1,
	)
	addFile(t, dir, &m, "events.jsonl", "events", "records", []byte(syntheticEvent+"\n"), 1)
	menu := []byte(
		`{"friday":{"lunch":[{"title_ru":"Суп","title_en":"Soup","price":185,"category":"soup"}],"dinner":[]}}`,
	)
	digest := sha256.Sum256(menu)
	addFile(t, dir, &m, "menu.json", "configuration", "resource", menu, 0)
	config := `{"_id":"food-config","kind":"legacy_food","event_key":"event_one","menu_file":"menu.json","menu_sha256":"` + hex.EncodeToString(
		digest[:],
	) + `","deadline":"2026-12-01T00:00:00Z","meal_prices":{"with_soup":665,"without_soup":555},"activity_prices":{"party":2000,"party_and_classes":2500,"all_classes":2000,"yoga":750,"cacao":1000,"soundhealing":1000},"cacao_capacity":38,"first_before_seconds":172800,"last_before_seconds":86400,"notify_after_seconds":300,"admins":[{"user_id":101,"can_export":true,"can_review":true,"can_assign":true,"instructions":{"en":"Synthetic","ru":"Тест"}}]}`
	addFile(t, dir, &m, "configuration.jsonl", "configuration", "records", []byte(config+"\n"), 1)
	order := `{"_id":{"$oid":"000000000000000000000001"},"user_id":101,"pass_key":"event_one","created_at":"2026-09-01T00:00:00Z","last_updated":"2026-09-02T00:00:00Z","order_details":{"friday":{"lunch":{"type":"individual-items","items":["0"]},"dinner":[]}},"origin_info":{"message_id":5,"chat_id":101},"total":184.99,"is_complete":true,"activities":{"open":true,"cacao":true},"proof_admin":101,"payment_status":"rejected","proof_file":"meal.pdf","proof_received_date":"2026-09-02T00:00:00Z","payment_rejected_by":101,"payment_rejected_date":"2026-09-03T00:00:00Z","activities_payment_status":"paid","activities_proof_file":"activity.jpg","activities_payment_confirmed_by":101,"activities_payment_confirmed_date":"2026-09-04T00:00:00Z","notification_first_sent":true}`
	addFile(t, dir, &m, "orders.jsonl", "orders", "records", []byte(order+"\n"), 1)
	addFile(t, dir, &m, "receipt.bin", "files", "blob", []byte("synthetic-food-receipt"), 0)
	m.Proofs = []migrate.Proof{
		{
			Source:         "orders",
			RecordID:       json.RawMessage(`{"$oid":"000000000000000000000001"}`),
			OwnerID:        json.RawMessage(`"user-a"`),
			Field:          "proof_file",
			TelegramFileID: "meal.pdf",
			Blob:           "receipt.bin",
		},
		{
			Source:         "orders",
			RecordID:       json.RawMessage(`{"$oid":"000000000000000000000001"}`),
			OwnerID:        json.RawMessage(`"user-a"`),
			Field:          "activities_proof_file",
			TelegramFileID: "activity.jpg",
			Unavailable:    true,
		},
	}
	for _, edit := range edits {
		edit(t, dir, &m)
	}
	writeManifest(t, dir, m)
	stage := filepath.Join(t.TempDir(), "stage")
	_, _, err := migrate.Stage(dir, stage, migrate.DefaultLimits())
	require.NoError(t, err)
	plan := filepath.Join(t.TempDir(), "food.json")
	summary, err := migrate.PlanFood(stage, plan, migrate.DefaultLimits())
	require.NoError(t, err)
	require.Zero(t, summary.Blocked)
	res := migrate.OrderResolutions{
		Version:               1,
		PlanSHA256:            summary.ArtifactSHA256,
		DatesVerified:         true,
		ConfigurationVerified: true,
		AdminGrantsVerified:   true,
		BotNamespaceVerified:  true,
		WritersStopped:        true,
	}
	raw, err := json.Marshal(res)
	require.NoError(t, err)
	resolution := filepath.Join(t.TempDir(), "resolution.json")
	require.NoError(t, os.WriteFile(resolution, raw, 0o600))
	return stage, plan, resolution
}

func TestFoodApplyConcurrentReplayAndRollback(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := foodImportInputs(t)
	applyOrderDependencies(t, dsn, stage)
	_, err := db.Exec(
		t.Context(),
		`CREATE FUNCTION public.fail_food_payment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic rollback'; END $$;
CREATE TRIGGER synthetic_food_failure BEFORE INSERT ON core.food_payments FOR EACH ROW EXECUTE FUNCTION public.fail_food_payment()`,
	)
	require.NoError(t, err)
	_, err = migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "food_payment_conflict")
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM core.food_events)+(SELECT count(*) FROM core.food_orders)+(SELECT count(*) FROM core.order_proofs)+(SELECT count(*) FROM core.legacy_food_import_references)`).
			Scan(&count),
	)
	require.Zero(t, count)
	_, err = db.Exec(
		t.Context(),
		`DROP TRIGGER synthetic_food_failure ON core.food_payments; DROP FUNCTION public.fail_food_payment()`,
	)
	require.NoError(t, err)
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() {
			_, applyErr := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			results <- applyErr
		})
	}
	group.Wait()
	close(results)
	for applyErr := range results {
		require.NoError(t, applyErr)
	}
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.food_orders`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = db.Exec(t.Context(), `UPDATE core.food_admins SET can_review=false`)
	require.NoError(t, err)
	_, err = migrate.ReconcileFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}

func TestFoodApplyPreservesIndependentPaymentsReplayAndDrift(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := foodImportInputs(t)
	applyOrderDependencies(t, dsn, stage)
	first, err := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	require.False(t, first.Reused)
	var total, activity int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT meal_total,activity_total FROM core.food_orders`).Scan(&total, &activity),
	)
	require.EqualValues(t, 18499, total)
	var origin string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT source_record->'origin_info' FROM core.legacy_food_import_references WHERE source_kind='food'`).
			Scan(&origin),
	)
	require.JSONEq(t, `{"message_id":5,"chat_id":101}`, origin)
	require.EqualValues(t, 250000, activity)
	var meal, paid string
	var absent bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (SELECT status FROM core.food_payments WHERE kind='meals'),(SELECT status FROM core.food_payments WHERE kind='activities'),(SELECT received_at IS NULL AND proof_id IS NULL FROM core.food_payments WHERE kind='activities')`).
			Scan(&meal, &paid, &absent),
	)
	require.Equal(t, "rejected", meal)
	require.Equal(t, "paid", paid)
	require.True(t, absent)
	var unknownReceivers int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.food_payments WHERE receiver IS NULL`).
			Scan(&unknownReceivers),
	)
	require.Equal(t, 2, unknownReceivers, "shared current routing does not prove either historical receiver")
	replay, err := migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	require.True(t, replay.Reused)
	_, err = migrate.ReconcileFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.food_orders SET activity_total=1`)
	require.NoError(t, err)
	_, err = migrate.ApplyFood(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_reconciliation_failed")
}
