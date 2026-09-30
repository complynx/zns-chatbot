package migrate_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	migrate "github.com/complynx/zns-chatbot/tools/migrate"
)

func TestMassageImportDeliveryBindings(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := massageImportInputs(t, nil)
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var markers, eligible, queued int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices
WHERE kind<>'additional' AND delivery_state='sent' AND sent_at=created_at
AND bot_id IS NULL AND delivery_chat=0 AND telegram_message_id=0 AND NOT followup_pending`).Scan(&markers))
	require.Equal(t, 2, markers, "source flag presence suppresses historical sends even when its value was false")
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices
WHERE kind='additional' AND owner='owner-202' AND bot_id=77 AND delivery_chat=202
AND delivery_state='pending' AND sent_at IS NULL`).Scan(&eligible))
	require.Equal(t, 1, eligible)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.delivery_queue q
JOIN core.massage_notices n ON q.owner_key=n.id::text
WHERE q.bot_id=n.bot_id AND q.owner_kind='massage' AND q.effect_key='send' AND q.chat='202'
AND q.thread_id=0 AND q.traffic_class='background' AND q.state='pending' AND q.lane_sequence=1`).Scan(&queued))
	require.Equal(t, 1, queued)
	var receipt string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT to_jsonb(r)::text FROM migrate_import.massage_receipts r`).Scan(&receipt),
	)
	summary, err := migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	require.True(t, summary.Reused)
	require.Zero(t, summary.Applied)
	var after string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT to_jsonb(r)::text FROM migrate_import.massage_receipts r`).Scan(&after),
	)
	require.Equal(t, receipt, after)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.delivery_queue`).Scan(&queued))
	require.Equal(t, 1, queued)
}

func TestMassageImportReconcilesDeliveryProgress(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := massageImportInputs(t, nil)
	applyOrderDependencies(t, dsn, stage)
	_, err := migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.NoError(t, err)
	var receipt string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT to_jsonb(r)::text FROM migrate_import.massage_receipts r`).Scan(&receipt),
	)
	// These are storage-contract checks. Actual worker transitions belong to
	// the separate CLI-to-runtime acceptance recipe, not this SQL fixture.
	for _, progress := range []string{
		`UPDATE core.massage_notices SET available_at=now()+interval '30 seconds',failure='telegram_cooldown',failure_count=1 WHERE kind='additional'; UPDATE core.delivery_queue SET not_before=now()+interval '30 seconds'`,
		`UPDATE core.massage_notices SET delivery_state='sending',delivery_attempt=1,lease_until=now()+interval '1 minute',delivery_text='synthetic' WHERE kind='additional'; UPDATE core.delivery_queue SET state='sending'`,
		`UPDATE core.massage_notices SET delivery_state='sent',sent_at=now(),telegram_message_id=123,followup_pending=true,lease_until=NULL WHERE kind='additional'; UPDATE core.delivery_queue SET state='sent'`,
		`UPDATE core.massage_notices SET followup_pending=false,followup_attempts=1,delivery_text='' WHERE kind='additional'`,
	} {
		_, err = db.Exec(t.Context(), progress)
		require.NoError(t, err)
		var before string
		require.NoError(t, db.QueryRow(t.Context(), massageDeliveryState).Scan(&before))
		summary, reconcileErr := migrate.ReconcileMassage(
			t.Context(),
			dsn,
			stage,
			plan,
			resolution,
			migrate.DefaultLimits(),
		)
		require.NoError(t, reconcileErr)
		require.True(t, summary.Reconciled)
		summary, err = migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
		require.NoError(t, err)
		require.True(t, summary.Reused)
		var after string
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT to_jsonb(r)::text FROM migrate_import.massage_receipts r`).Scan(&after),
		)
		require.Equal(t, receipt, after, "verification never rewrites the original import receipt")
		require.NoError(t, db.QueryRow(t.Context(), massageDeliveryState).Scan(&after))
		require.Equal(t, before, after, "replay preserves delivery progress")
	}
}

func TestMassageImportRejectsDeliveryBindingDrift(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]string{
		"owner":                `UPDATE core.massage_notices SET owner='owner-101' WHERE kind='additional'`,
		"bot":                  `UPDATE core.massage_notices SET bot_id=78 WHERE kind='additional'`,
		"chat":                 `UPDATE core.massage_notices SET delivery_chat=101 WHERE kind='additional'`,
		"historical state":     `UPDATE core.massage_notices SET delivery_state='pending' WHERE kind='prior_long'`,
		"historical timestamp": `UPDATE core.massage_notices SET sent_at=sent_at+interval '1 second' WHERE kind='prior_long'`,
		"missing notice":       `DELETE FROM core.massage_notices WHERE kind='additional'`,
		"missing queue":        `DELETE FROM core.delivery_queue`,
		"queue sequence":       `UPDATE core.delivery_queue SET lane_sequence=2`,
		"queue destination":    `UPDATE core.delivery_queue SET thread_id=1`,
		"queue effect":         `UPDATE core.delivery_queue SET effect_key='other'`,
		"queue class":          `UPDATE core.delivery_queue SET traffic_class='interactive'`,
		"source":               `UPDATE core.legacy_massage_import_references SET source_record='{}'::jsonb WHERE source_kind='booking'`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			stage, plan, resolution := massageImportInputs(t, nil)
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			changed, err := db.Exec(t.Context(), change)
			require.NoError(t, err)
			require.EqualValues(t, 1, changed.RowsAffected())
			var before string
			require.NoError(t, db.QueryRow(t.Context(), massageDeliveryState).Scan(&before))
			_, err = migrate.ReconcileMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.EqualError(t, err, "apply_reconciliation_failed")
			_, err = migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.EqualError(t, err, "apply_reconciliation_failed")
			var after string
			require.NoError(t, db.QueryRow(t.Context(), massageDeliveryState).Scan(&after))
			require.Equal(t, before, after, "a mismatch is never repaired")
		})
	}
}

const massageDeliveryState = `SELECT jsonb_build_object(
 'notices',(SELECT jsonb_agg(to_jsonb(n) ORDER BY id) FROM core.massage_notices n),
 'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY bot_id,owner_kind,owner_key,effect_key) FROM core.delivery_queue q),
 'receipts',(SELECT jsonb_agg(to_jsonb(r)) FROM migrate_import.massage_receipts r),
 'references',(SELECT jsonb_agg(to_jsonb(r) ORDER BY source_key) FROM core.legacy_massage_import_references r))::text`

func TestMassageImportRollsBackQueueWithReceiptFailure(t *testing.T) {
	t.Parallel()
	dsn, db := applyDatabase(t)
	stage, plan, resolution := massageImportInputs(t, nil)
	applyOrderDependencies(t, dsn, stage)
	_, err := db.Exec(t.Context(), `CREATE TABLE migrate_import.massage_receipts(
manifest_sha256 text PRIMARY KEY, plan_sha256 text NOT NULL,resolution_sha256 text NOT NULL,
owners jsonb NOT NULL,notice_ids bigint[] NOT NULL,snapshot jsonb NOT NULL,
CONSTRAINT reject_receipt CHECK(false))`)
	require.NoError(t, err)
	_, err = migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
	require.EqualError(t, err, "apply_receipt_conflict")
	var rows int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
(SELECT count(*) FROM core.massage_notices)+(SELECT count(*) FROM core.massage_bookings)+
(SELECT count(*) FROM core.delivery_queue)+(SELECT count(*) FROM core.delivery_lanes)+
(SELECT count(*) FROM core.legacy_massage_import_references)+(SELECT count(*) FROM migrate_import.massage_receipts)`).Scan(&rows))
	require.Zero(t, rows, "queue and owner rows share the failed importer transaction")
}

func TestMassageImportDeliveryNoEligibleAdditional(t *testing.T) {
	t.Parallel()
	for name, replacement := range map[string]string{
		"not requested":  `"notify":false`,
		"deleted marker": `"notify":true,"deleted":false`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dsn, db := applyDatabase(t)
			stage, plan, resolution := massageImportInputs(t, func(records string) string {
				return strings.ReplaceAll(records, `"notify":true`, replacement)
			})
			applyOrderDependencies(t, dsn, stage)
			_, err := migrate.ApplyMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
			var count int
			require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.delivery_queue`).Scan(&count))
			require.Zero(t, count)
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.massage_notices WHERE kind='additional'`).
					Scan(&count),
			)
			require.Zero(t, count)
			_, err = migrate.ReconcileMassage(t.Context(), dsn, stage, plan, resolution, migrate.DefaultLimits())
			require.NoError(t, err)
		})
	}
}
