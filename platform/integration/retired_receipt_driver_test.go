package integration_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
)

// Count matching occurrences while retaining the existing real pgx deadline injector.
type r100SQLTrace struct {
	*scriptSQLTrace

	occurrence int32
	matches    atomic.Int32
}

func (trace *r100SQLTrace) TraceQueryStart(
	ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if strings.Join(strings.Fields(data.SQL), " ") != trace.statement ||
		trace.matches.Add(1) != trace.occurrence {
		return ctx
	}
	return trace.scriptSQLTrace.TraceQueryStart(ctx, conn, data)
}

func r100SQLPool(t *testing.T, db *pgxpool.Pool, trace *r100SQLTrace) *pgxpool.Pool {
	t.Helper()
	config := db.Config().Copy()
	config.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

type r100SQLCase struct {
	name, statement string
	occurrence      int32
}

func TestRetiredReceiptRealDriverSQLShapes(t *testing.T) {
	t.Parallel()
	for family, cases := range r100SQLCases() {
		t.Run(family, func(t *testing.T) {
			t.Parallel()
			healthy, input := r100CanonicalReceipt(t, family)
			before, err := healthy.ReadPassOperation(t.Context(), "bob", input)
			require.NoError(t, err, "the canonical fixture must be readable before injecting SQL failure")
			require.Contains(t, []string{"committed", "complete"}, before.Summary.Status)
			if input.Batch != nil {
				require.Equal(t, 1, before.Summary.Committed)
			}
			stored := r100ReceiptSnapshot(t, healthy.DB)
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					t.Parallel()
					trace := &r100SQLTrace{
						scriptSQLTrace: &scriptSQLTrace{statement: strings.Join(strings.Fields(test.statement), " ")},
						occurrence:     test.occurrence,
					}
					pool := r100SQLPool(t, healthy.DB, trace)
					faulty := healthy
					faulty.DB, faulty.Registration.DB = pool, pool
					got, readErr := faulty.ReadPassOperation(t.Context(), "bob", input)
					requireScriptLocalSQL(t, trace.scriptSQLTrace, readErr)
					require.Equal(
						t,
						test.occurrence,
						trace.matches.Load(),
						"selected occurrence must be reached exactly",
					)
					require.EqualError(t, readErr, "database unavailable")
					require.Equal(t, derivedmutation.PassOperationRead{}, got, "no partial receipt on failure")
					require.Equal(t, stored, r100ReceiptSnapshot(t, healthy.DB))
					after, recoveryErr := healthy.ReadPassOperation(t.Context(), "bob", input)
					require.NoError(t, recoveryErr)
					require.Equal(t, before, after, "recovery observes the same canonical receipt without replay")
					require.Equal(t, stored, r100ReceiptSnapshot(t, healthy.DB))
				})
			}
		})
	}
}

func r100SQLCases() map[string][]r100SQLCase {
	const actor = `SELECT telegram_id,can_book FROM core.users WHERE id=$1 FOR SHARE`
	const admin = `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`
	const fallback = `SELECT owner FROM core.pass_payment_admins WHERE owner=$1 AND event_id=$2 FOR SHARE`
	return map[string][]r100SQLCase{
		"command": {
			{
				"mutation events",
				`SELECT id FROM core.pass_events WHERE id=ANY($1::text[]) ORDER BY id FOR NO KEY UPDATE`,
				1,
			},
			{
				"mutation actors",
				`SELECT id FROM core.users WHERE id=ANY($1::text[]) OR telegram_id=ANY($2::bigint[]) ORDER BY id FOR NO KEY UPDATE`,
				1,
			},
			{
				"event",
				`SELECT finishes_at,passport_required,assignment_rule,disable_concurrency_limit FROM core.pass_events WHERE id=$1 FOR NO KEY UPDATE`,
				1,
			},
			{
				"tiers",
				`SELECT amount,price,starts_at,promo,blocked_by_date FROM core.pass_event_tiers WHERE event_id=$1 ORDER BY position`,
				1,
			},
			{
				"tier positions",
				`SELECT EXISTS(SELECT 1 FROM (SELECT position,row_number() OVER(ORDER BY position)-1 expected FROM core.pass_event_tiers WHERE event_id=$1) t WHERE position<>expected)`,
				1,
			},
			{"payment admins", `SELECT owner,hidden FROM core.pass_payment_admins WHERE event_id=$1 ORDER BY owner`, 1},
			{"receipt actor", actor, 1},
			{"capability actor", actor, 2},
			{"target actor", actor, 3},
			{"cancel primary grant", admin, 1},
			{"target identity", `SELECT telegram_id FROM core.users WHERE id=$1 FOR SHARE`, 1},
			{"read events", `-- name: LockReadEvents :many
SELECT id FROM core.pass_events WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE`, 1},
			{"read actor", `-- name: LockReadActor :one
SELECT telegram_id FROM core.users WHERE id=$1 FOR SHARE`, 1},
			{"read target", `-- name: LockReadTarget :one
SELECT id,can_book FROM core.users WHERE telegram_id=$1 FOR SHARE`, 1},
			{"target booking", `-- name: LockReadBooking :one
SELECT owner,version,created_at,state,invitation_target,COALESCE(payment_attempt,'') AS payment_attempt
FROM core.pass_bookings WHERE event_id=$1 AND owner=$2 FOR SHARE`, 1},
			{
				"command receipt",
				`SELECT request_hash FROM core.pass_booking_operations WHERE actor=$1 AND event_id=$2 AND key_hash=$3`,
				1,
			},
		},
		"command fallback":  {{"cancel fallback grant", fallback, 1}},
		"assignment":        {{"assignment primary grant", admin, 1}},
		"takeover":          {{"takeover primary grant", admin, 1}},
		"takeover fallback": {{"takeover fallback grant", fallback, 1}},
		"proof": {
			{"proof grant", `SELECT owner FROM core.pass_payment_admins WHERE event_id=$1 AND owner=$2 FOR SHARE`, 1},
		},
		"batch": {
			{
				"pre-transaction plan",
				`SELECT request_hash,plan,source_derivation FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2`,
				1,
			},
			{
				"locked plan",
				`SELECT plan,source_derivation FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2 FOR UPDATE`,
				1,
			},
			{"locked plan actor", actor, 2},
			{"locked plan grant", admin, 2},
			{"batch receipt", `SELECT request_hash FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2`, 1},
			{"assignment transitions", `SELECT r.before_records,r.after_records
FROM core.pass_admin_assignments r JOIN core.pass_booking_operations o
ON o.event_id=r.event_id AND o.actor=r.actor AND o.key_hash=r.key_hash
WHERE r.event_id=$1 AND r.actor=$2 AND r.key_hash=$3 AND r.target=$4 AND o.request_hash=$5`, 1},
		},
		"cancel batch": {
			{
				"item receipt",
				`SELECT EXISTS(SELECT 1 FROM core.pass_booking_operations WHERE actor=$1 AND event_id=$2 AND key_hash=$3 AND request_hash=$4)`,
				1,
			},
		},
	}
}

func r100ReceiptSnapshot(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var stored string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'operations',(SELECT coalesce(jsonb_agg(to_jsonb(o) ORDER BY actor,event_id,key_hash),'[]'::jsonb) FROM core.pass_booking_operations o),
 'bookings',(SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY event_id,owner),'[]'::jsonb) FROM core.pass_bookings b),
 'assignments',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY actor,event_id,key_hash),'[]'::jsonb) FROM core.pass_admin_assignments a),
 'batches',(SELECT coalesce(jsonb_agg(to_jsonb(b) ORDER BY actor,key_hash),'[]'::jsonb) FROM core.pass_admin_batches b),
 'payments',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY event_id,id),'[]'::jsonb) FROM core.pass_payment_attempts p)
)::text`).Scan(&stored))
	return stored
}
