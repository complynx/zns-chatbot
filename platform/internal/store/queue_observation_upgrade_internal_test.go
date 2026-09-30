package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestDeliveryQueueObservationUpgradeFrom087(t *testing.T) {
	t.Parallel()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required in CI")
		}
		t.Skip("TEST_DATABASE_URL required for isolated upgrade proof")
	}
	admin, err := pgxpool.New(t.Context(), url)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	suffix := make([]byte, 8)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := "synthetic_qa_queue_upgrade_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(url)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()
		_, dropErr := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)")
		require.NoError(t, dropErr)
	})
	// Construct the real pre-088 schema from actual embedded forward migrations,
	// recording the same immutable checksums used by the product migrator.
	_, err = db.Exec(t.Context(), `CREATE TABLE public.zns_schema_migrations
 (name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`)
	require.NoError(t, err)
	entries, err := migrations.ReadDir("migrations")
	require.NoError(t, err)
	const lastOld = "087_telegram_inbox_retries.sql"
	const upgrade = "088_delivery_queue_observation.sql"
	const current = "089_credit_usage_observation.sql"
	foundOld, foundUpgrade := false, false
	expectedOldLedger := make([]queueUpgradeLedgerEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() > current {
			t.Fatalf(
				"upgrade proof is pinned to the 089 embedded schema epoch; newer migration %s requires a new scoped fixture",
				entry.Name(),
			)
		}
		if entry.Name() == upgrade {
			foundUpgrade = true
		}
		if entry.Name() > lastOld {
			continue
		}
		body, readErr := migrations.ReadFile("migrations/" + entry.Name())
		require.NoError(t, readErr)
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		expectedOldLedger = append(expectedOldLedger, queueUpgradeLedgerEntry{Name: entry.Name(), Checksum: checksum})
		tx, beginErr := db.Begin(t.Context())
		require.NoError(t, beginErr)
		_, applyErr := tx.Exec(t.Context(), string(body))
		if applyErr != nil {
			_ = tx.Rollback(t.Context())
			require.NoError(t, applyErr, entry.Name())
		}
		_, recordErr := tx.Exec(
			t.Context(),
			`INSERT INTO public.zns_schema_migrations(name,checksum) VALUES($1,$2)`,
			entry.Name(),
			checksum,
		)
		if recordErr != nil {
			_ = tx.Rollback(t.Context())
			require.NoError(t, recordErr)
		}
		require.NoError(t, tx.Commit(t.Context()))
		if entry.Name() == lastOld {
			foundOld = true
		}
	}
	require.True(t, foundOld)
	require.True(t, foundUpgrade)
	require.Equal(
		t,
		expectedOldLedger,
		queueUpgradeLedger(t, db),
		"exact through-087 ledger names and actual embedded byte checksums",
	)
	oldLedgerSnapshot := queueUpgradeLedgerSnapshot(t, db, lastOld)
	upgradeBody, err := migrations.ReadFile("migrations/" + upgrade)
	require.NoError(t, err)
	expectedUpgradedLedger := append(
		append([]queueUpgradeLedgerEntry(nil), expectedOldLedger...),
		queueUpgradeLedgerEntry{Name: upgrade, Checksum: fmt.Sprintf("%x", sha256.Sum256(upgradeBody))},
	)
	currentBody, err := migrations.ReadFile("migrations/" + current)
	require.NoError(t, err)
	expectedUpgradedLedger = append(expectedUpgradedLedger,
		queueUpgradeLedgerEntry{Name: current, Checksum: fmt.Sprintf("%x", sha256.Sum256(currentBody))})
	var hadTimestamp bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='core' AND table_name='delivery_queue' AND column_name='enqueued_at')`).
			Scan(&hadTimestamp),
	)
	require.False(t, hadTimestamp, "old schema must be genuine, not a modified current-schema fixture")
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.delivery_lanes(bot_id,chat,next_sequence,last_served) VALUES(4242,'101',3,11);
 INSERT INTO core.delivery_fairness(bot_id,grants) VALUES(4242,19);
 INSERT INTO core.delivery_pacing(bot_id,chat,not_before,pause_reason) VALUES(4242,'','infinity','telegram_invalid_cooldown');
 INSERT INTO core.delivery_queue(bot_id,owner_kind,owner_key,effect_key,chat,thread_id,lane_sequence,traffic_class,state,not_before)
 VALUES(4242,'orders','pending','notice','101',7,1,'interactive','pending','infinity'),
 (4242,'orders','sending','notice','101',7,2,'interactive','sending',statement_timestamp()+interval '1 hour'),
 (4242,'orders','uncertain','notice','101',7,3,'background','unknown','-infinity');
 INSERT INTO core.users(id,telegram_id,name) VALUES('upgrade-actor',987654,'Synthetic');
 INSERT INTO core.admin_messages(actor,key,request,state) VALUES('upgrade-actor','old-job','{}','queued');
 INSERT INTO core.admin_message_deliveries(message_id,destination,state,attempt,failure_count,bot_id,available_at,lease_until,failure)
 SELECT id,'{"chat":"101"}','pending',7,4,4242,'infinity',statement_timestamp()+interval '1 hour','telegram_rate_limit' FROM core.admin_messages`,
	)
	require.NoError(t, err)
	// Compare the exact pre-upgrade durable scheduling state, not invented values.
	before := queueUpgradeState(t, db, false)
	require.NoError(t, Migrate(t.Context(), db))
	require.Equal(
		t,
		expectedUpgradedLedger,
		queueUpgradeLedger(t, db),
		"only actual 088 and 089 ledger entries are added; old entries remain exact",
	)
	require.JSONEq(
		t,
		oldLedgerSnapshot,
		queueUpgradeLedgerSnapshot(t, db, lastOld),
		"the upgrade must preserve every old ledger field including applied timestamps",
	)
	upgradedLedgerSnapshot := queueUpgradeLedgerSnapshot(t, db, current)
	after := queueUpgradeState(t, db, true)
	require.JSONEq(t, before, after, "all existing order/retry/cooldown metadata must survive")
	var unknown int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.delivery_queue WHERE enqueued_at IS NULL`).Scan(&unknown),
	)
	require.Equal(t, int64(3), unknown)
	items, err := delivery.QueueObservations(t.Context(), db, 4242)
	require.NoError(t, err)
	require.Len(t, items, 3)
	for _, item := range items {
		require.Equal(t, int64(1), item.UnknownAge)
		require.Zero(t, item.OldestAgeSeconds)
		require.Equal(t, int64(1), item.UnboundedDeadline)
	}
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	ref := delivery.Reference{Owner: delivery.Food, Key: "new-row", Effect: "notice"}
	_, err = delivery.Register(t.Context(), tx, 4242, ref, delivery.Destination{Chat: "101"}, delivery.Background)
	if err != nil {
		_ = tx.Rollback(t.Context())
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(t.Context()))
	require.NoError(t, err)
	var original time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT enqueued_at FROM core.delivery_queue WHERE owner_key='new-row'`).
			Scan(&original),
	)
	require.False(t, original.IsZero())
	tx, err = db.Begin(t.Context())
	require.NoError(t, err)
	_, err = delivery.Register(t.Context(), tx, 4242, ref, delivery.Destination{Chat: "101"}, delivery.Background)
	if err != nil {
		_ = tx.Rollback(t.Context())
		require.NoError(t, err)
	}
	err = delivery.Project(t.Context(), tx, 4242, ref, delivery.Deferred, time.Now().Add(time.Hour))
	if err != nil {
		_ = tx.Rollback(t.Context())
		require.NoError(t, err)
	}
	require.NoError(t, tx.Commit(t.Context()))
	var retained time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT enqueued_at FROM core.delivery_queue WHERE owner_key='new-row'`).
			Scan(&retained),
	)
	require.Equal(t, original, retained, "retry projection retains the initial enqueue age")
	require.NoError(t, Migrate(t.Context(), db), "restart migration is idempotent")
	require.Equal(
		t,
		expectedUpgradedLedger,
		queueUpgradeLedger(t, db),
		"replay preserves exact migration versions and checksums",
	)
	require.JSONEq(
		t,
		upgradedLedgerSnapshot,
		queueUpgradeLedgerSnapshot(t, db, current),
		"replay preserves the complete ledger including applied timestamps",
	)
}

type queueUpgradeLedgerEntry struct {
	Name     string
	Checksum string
}

func queueUpgradeLedger(t *testing.T, db *pgxpool.Pool) []queueUpgradeLedgerEntry {
	t.Helper()
	rows, err := db.Query(t.Context(), `SELECT name,checksum FROM public.zns_schema_migrations ORDER BY name`)
	require.NoError(t, err)
	entries, err := pgx.CollectRows(rows, pgx.RowToStructByPos[queueUpgradeLedgerEntry])
	require.NoError(t, err)
	return entries
}

func queueUpgradeLedgerSnapshot(t *testing.T, db *pgxpool.Pool, through string) string {
	t.Helper()
	var snapshot string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(m) ORDER BY m.name)::text FROM public.zns_schema_migrations m WHERE m.name<=$1`, through).
			Scan(&snapshot),
	)
	return snapshot
}

func queueUpgradeState(t *testing.T, db *pgxpool.Pool, after bool) string {
	t.Helper()
	queueExpression := "to_jsonb(q)"
	if after {
		queueExpression = "to_jsonb(q)-'enqueued_at'"
	}
	query := `SELECT jsonb_build_object(
 'queue',(SELECT jsonb_agg(` + queueExpression + ` ORDER BY q.lane_sequence) FROM core.delivery_queue q),
 'lanes',(SELECT jsonb_agg(to_jsonb(l) ORDER BY l.chat) FROM core.delivery_lanes l),
 'pacing',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.chat) FROM core.delivery_pacing p),
 'fairness',(SELECT jsonb_agg(to_jsonb(f) ORDER BY f.bot_id) FROM core.delivery_fairness f),
 'jobs',(SELECT jsonb_agg(to_jsonb(m) ORDER BY m.id) FROM core.admin_messages m),
 'attempts',(SELECT jsonb_agg(to_jsonb(d) ORDER BY d.id) FROM core.admin_message_deliveries d))::text`
	var value string
	require.NoError(t, db.QueryRow(t.Context(), query).Scan(&value))
	return value
}
