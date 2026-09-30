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

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

const creditObservationUpgrade = "089_credit_usage_observation.sql"

func TestCreditObservationUpgradeFrom088(t *testing.T) {
	t.Parallel()
	db := creditObservationUpgradeDatabase(t)
	expectedOld := applyCreditObservationPredecessor(t, db)
	require.Equal(t, expectedOld, queueUpgradeLedger(t, db))
	oldLedger := queueUpgradeLedgerSnapshot(t, db, "088_delivery_queue_observation.sql")
	seedCreditObservationUpgrade(t, db)
	before := creditObservationUpgradeState(t, db)
	var indexPresent bool
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT to_regclass('credits.credit_attempt_observation_time') IS NOT NULL`).Scan(&indexPresent))
	require.False(t, indexPresent, "the actual 088 schema must precede the new index")
	body, err := migrations.ReadFile("migrations/" + creditObservationUpgrade)
	require.NoError(t, err)
	expected := append([]queueUpgradeLedgerEntry(nil), expectedOld...)
	expected = append(expected, queueUpgradeLedgerEntry{
		Name: creditObservationUpgrade, Checksum: fmt.Sprintf("%x", sha256.Sum256(body)),
	})
	require.NoError(t, Migrate(t.Context(), db))
	require.Equal(t, expected, queueUpgradeLedger(t, db), "only the exact 089 checksum entry is added")
	require.JSONEq(t, oldLedger, queueUpgradeLedgerSnapshot(t, db, "088_delivery_queue_observation.sql"),
		"all predecessor ledger fields including applied_at remain unchanged")
	require.JSONEq(t, before, creditObservationUpgradeState(t, db),
		"index creation preserves exact durable accounting rows, costs and timestamps")
	var definition string
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT indexdef FROM pg_indexes WHERE schemaname='credits' AND indexname='credit_attempt_observation_time'`).
		Scan(&definition))
	require.Equal(
		t,
		"CREATE INDEX credit_attempt_observation_time ON credits.attempts USING btree (created_at DESC, id DESC)",
		definition,
		"index keys match the bounded observation predicate and deterministic order",
	)
	item, err := (credits.Service{DB: db}).ObserveUsage(t.Context())
	require.NoError(t, err)
	require.Equal(t, [4]int64{1, 1, 2, 1}, item.States)
	require.Equal(t, [4]int64{1, 0, 1, 0}, item.Bases)
	require.Equal(t, credits.UsageTokenObservation{Sum: 17, KnownReceipts: 1, UnknownReceipts: 1}, item.Tokens[0])
	upgradedLedger := queueUpgradeLedgerSnapshot(t, db, creditObservationUpgrade)
	require.NoError(t, Migrate(t.Context(), db))
	require.Equal(t, expected, queueUpgradeLedger(t, db))
	require.JSONEq(t, upgradedLedger, queueUpgradeLedgerSnapshot(t, db, creditObservationUpgrade))
	require.JSONEq(t, before, creditObservationUpgradeState(t, db),
		"scrape and migration replay preserve every accounting field")
	checkCreditObservationIndexPlan(t, db, item)
}

func creditObservationUpgradeDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	address := os.Getenv("TEST_DATABASE_URL")
	if address == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required in CI")
		}
		t.Skip("TEST_DATABASE_URL required for isolated credit observation upgrade")
	}
	admin, err := pgxpool.New(t.Context(), address)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	suffix := make([]byte, 8)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := "synthetic_qa_zns_credit_upgrade_" + hex.EncodeToString(suffix)
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	options, err := pgxpool.ParseConfig(address)
	require.NoError(t, err)
	options.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), options)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
		defer cancel()
		_, dropErr := admin.Exec(ctx, "DROP DATABASE "+quoted+" WITH (FORCE)")
		require.NoError(t, dropErr)
	})
	return db
}

func applyCreditObservationPredecessor(t *testing.T, db *pgxpool.Pool) []queueUpgradeLedgerEntry {
	t.Helper()
	entries, err := migrations.ReadDir("migrations")
	require.NoError(t, err)
	require.Equal(t, creditObservationUpgrade, entries[len(entries)-1].Name(),
		"upgrade proof is pinned to the 089 embedded schema epoch")
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = tx.Exec(t.Context(), `CREATE TABLE public.zns_schema_migrations
 (name text PRIMARY KEY,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`)
	require.NoError(t, err)
	expected := make([]queueUpgradeLedgerEntry, 0, len(entries)-1)
	for _, entry := range entries {
		if entry.Name() == creditObservationUpgrade {
			continue
		}
		body, readErr := migrations.ReadFile("migrations/" + entry.Name())
		require.NoError(t, readErr)
		_, err = tx.Exec(t.Context(), string(body))
		require.NoError(t, err, entry.Name())
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		_, err = tx.Exec(t.Context(),
			`INSERT INTO public.zns_schema_migrations(name,checksum) VALUES($1,$2)`, entry.Name(), checksum)
		require.NoError(t, err)
		expected = append(expected, queueUpgradeLedgerEntry{Name: entry.Name(), Checksum: checksum})
	}
	require.Equal(t, "088_delivery_queue_observation.sql", expected[len(expected)-1].Name)
	require.NoError(t, tx.Commit(t.Context()))
	return expected
}

func seedCreditObservationUpgrade(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	service := credits.Service{DB: db}
	for _, state := range []string{"reserved", "dispatched", "settled", "unknown", "not_sent"} {
		id := uuid.NewString()
		reserved := int64(59)
		require.NoError(t, service.Reserve(t.Context(), credits.Attempt{
			ID: id, Scope: credits.Scope{Actor: "synthetic-actor", Payer: "synthetic-payer", Key: id},
			Operation: "synthetic-operation", Provider: "synthetic-provider", Model: "synthetic-model",
			ReservedNanoUSD: &reserved,
		}))
		if state == "not_sent" {
			require.NoError(t, service.NotSent(t.Context(), id))
			continue
		}
		if state == "reserved" {
			continue
		}
		require.NoError(t, service.Dispatch(t.Context(), id))
		if state == "dispatched" {
			continue
		}
		usage := credits.Usage{Basis: "unknown"}
		if state == "settled" {
			input := int64(17)
			usage = credits.Usage{Basis: "reported", Input: &input, RequestID: "synthetic-receipt"}
		}
		settlement := credits.Settlement{Usage: usage, CostBasis: "unknown"}
		if state == "settled" {
			cost := int64(57)
			settlement.CostNanoUSD = &cost
			settlement.CostBasis = "provider_reported"
		}
		require.NoError(t, service.Settle(t.Context(), id, settlement))
	}
}

func creditObservationUpgradeState(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var raw string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'attempts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.id) FROM credits.attempts a),
 'accounts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY a.payer) FROM credits.accounts a),
 'policy',(SELECT jsonb_agg(to_jsonb(p)) FROM credits.default_policy p))::text`).Scan(&raw))
	return raw
}

type creditObservationQuery struct {
	sql  string
	args []any
}

type creditObservationTrace struct{ query chan creditObservationQuery }

func (trace creditObservationTrace) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	select {
	case trace.query <- creditObservationQuery{sql: data.SQL, args: append([]any(nil), data.Args...)}:
	default:
	}
	return ctx
}

func (creditObservationTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func checkCreditObservationIndexPlan(t *testing.T, db *pgxpool.Pool, expected credits.UsageObservation) {
	t.Helper()
	_, err := db.Exec(t.Context(), `INSERT INTO credits.attempts
 (id,operation_key,actor,payer,operation,provider,model,state,created_at)
 SELECT md5('index-history-'||n)::uuid,'synthetic','synthetic','synthetic','synthetic','synthetic','synthetic',
 'reserved',statement_timestamp()-interval '2 days' FROM generate_series(1,10000) n`)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), "ANALYZE credits.attempts")
	require.NoError(t, err)
	query := make(chan creditObservationQuery, 1)
	options := db.Config().Copy()
	options.ConnConfig.Tracer = creditObservationTrace{query: query}
	observedDB, err := pgxpool.NewWithConfig(t.Context(), options)
	require.NoError(t, err)
	t.Cleanup(observedDB.Close)
	item, err := (credits.Service{DB: observedDB}).ObserveUsage(t.Context())
	require.NoError(t, err)
	require.Equal(t, expected, item, "older history does not contribute to the recent sample")
	var captured creditObservationQuery
	select {
	case captured = <-query:
	default:
		t.Fatal("actual observation query was not traced")
	}
	var plan string
	require.NoError(t, db.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) "+captured.sql, captured.args...).Scan(&plan))
	require.Contains(t, plan, `"Index Name": "credit_attempt_observation_time"`,
		"the actual typed observation query uses the additive index with a large older history")
}
