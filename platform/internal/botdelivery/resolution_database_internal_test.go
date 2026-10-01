package botdelivery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestResolutionPostgresExactEvidenceAndReplay(t *testing.T) {
	t.Parallel()
	db := storedJSONDatabase(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, "INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING")
	require.NoError(t, err)
	s := Service{
		DB: db,
		Delivery: delivery.Settings{
			BotID:        999,
			BotInterval:  time.Millisecond,
			ChatInterval: time.Millisecond,
			Fallback:     time.Second,
		},
	}
	ref := Reference{Kind: IdentityIntent, Update: 1, Notice: i18n.IdentityUnavailable, Language: "en"}
	obs, err := s.Enqueue(
		ctx,
		EnqueueRequest{Chat: 101, Operation: "first", Effect: "notice", Reference: ref, Phase: phaseSend},
	)
	require.NoError(t, err)
	i, err := Read(ctx, db, 999, obs.Reference, false)
	require.NoError(t, err)
	prepared := PreparedAttempt{
		Method:       "sendMessage",
		SHA256:       strings.Repeat("a", 64),
		Continuation: Continuation{Tokens: []string{"private-canary"}},
	}
	admitted, err := s.Begin(ctx, BeginRequest{Observed: i, Prepared: &prepared})
	require.NoError(t, err)
	require.True(t, admitted.Ready)
	// Original continuation is durable while the wire is still in flight.
	var privateSaved bool
	require.NoError(
		t,
		db.QueryRow(ctx, "SELECT continuation->'tokens' IS NOT NULL FROM bot.delivery_attempts WHERE operation_key='first'").
			Scan(&privateSaved),
	)
	require.True(t, privateSaved)
	require.NoError(
		t,
		s.Complete(
			ctx,
			CompletionRequest{
				Attempt: admitted.Intent,
				Outcome: delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"},
				Receipt: prepared.Continuation,
			},
		),
	)
	ref.Update = 2
	_, err = s.Enqueue(
		ctx,
		EnqueueRequest{Chat: 101, Operation: "second", Effect: "notice", Reference: ref, Phase: phaseSend},
	)
	require.NoError(t, err)
	key := IntentKey{Operation: "first", Effect: "notice"}
	before, err := s.Inspect(ctx, "alice", key)
	require.NoError(t, err)
	require.Equal(t, delivery.Uncertain, before.State)
	require.Equal(t, int64(101), before.Chat)
	require.Equal(t, "sendMessage", before.Method)
	require.Equal(t, int64(1), before.BlockedFollowers)
	encoded, err := json.Marshal(before)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-canary")
	_, err = s.Inspect(ctx, "bob", key)
	require.Error(t, err)
	r := Resolution{
		IntentKey:      key,
		Key:            "resolve1",
		Attempt:        1,
		Disposition:    "confirmed_sent",
		EvidenceKind:   "provider_receipt",
		EvidenceSHA256: strings.Repeat("b", 64),
		PayloadSHA256:  prepared.SHA256,
		MessageID:      42,
		Joined:         true,
		Quiescent:      true,
	}
	wrong := r
	_, err = s.Resolve(ctx, "bob", r)
	require.Error(t, err)
	wrong.PayloadSHA256 = strings.Repeat("c", 64)
	_, err = s.Resolve(ctx, "alice", wrong)
	require.ErrorIs(t, err, ErrBinding)
	// A real active bot lock blocks even a correctly formed disposition.
	owner, err := db.Acquire(ctx)
	require.NoError(t, err)
	_, err = owner.Exec(ctx, "SELECT pg_advisory_lock(918273)")
	require.NoError(t, err)
	_, err = s.Resolve(ctx, "alice", r)
	require.ErrorIs(t, err, ErrBinding)
	_, err = owner.Exec(ctx, "SELECT pg_advisory_unlock(918273)")
	require.NoError(t, err)
	owner.Release()
	// Audit storage failure must roll back both the intent and shared queue outcome.
	_, err = db.Exec(ctx, `CREATE FUNCTION bot.reject_resolution_audit() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'synthetic audit storage failure'; END $$;
 CREATE TRIGGER reject_resolution_audit BEFORE INSERT ON bot.delivery_resolutions
 FOR EACH ROW EXECUTE FUNCTION bot.reject_resolution_audit()`)
	require.NoError(t, err)
	_, err = s.Resolve(ctx, "alice", r)
	require.Error(t, err)
	unchanged, err := s.Inspect(ctx, "alice", key)
	require.NoError(t, err)
	require.Equal(t, delivery.Uncertain, unchanged.State)
	var projected string
	require.NoError(
		t,
		db.QueryRow(ctx, "SELECT state FROM core.delivery_queue WHERE owner_key='first'").Scan(&projected),
	)
	require.Equal(t, "unknown", projected)
	_, err = db.Exec(
		ctx,
		"DROP TRIGGER reject_resolution_audit ON bot.delivery_resolutions; DROP FUNCTION bot.reject_resolution_audit()",
	)
	require.NoError(t, err)
	result, err := s.Resolve(ctx, "alice", r)
	require.NoError(t, err)
	require.Equal(t, delivery.Succeeded, result.State)
	require.Equal(t, int64(42), result.MessageID)
	visible, err := s.Inspect(ctx, "alice", key)
	require.NoError(t, err)
	require.Equal(t, "confirmed_sent", visible.Disposition)
	require.Zero(t, visible.BlockedFollowers)
	replay, err := s.Resolve(ctx, "alice", r)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	r.MessageID = 43
	_, err = s.Resolve(ctx, "alice", r)
	require.ErrorIs(t, err, ErrBinding)
	r.MessageID = 42
	_, err = db.Exec(ctx, "DELETE FROM core.pass_booking_admins WHERE owner='alice'")
	require.NoError(t, err)
	_, err = s.Resolve(ctx, "alice", r)
	require.Error(t, err)
	var originalState, originalReason string
	require.NoError(
		t,
		db.QueryRow(ctx, "SELECT result->>'original_state',result->>'original_reason' FROM bot.delivery_resolutions").
			Scan(&originalState, &originalReason),
	)
	require.Equal(t, "unknown", originalState)
	require.Equal(t, "telegram_outcome_unknown", originalReason)
}

func TestResolutionPostgresUnsentAndLegacyRemainDistinct(t *testing.T) {
	t.Parallel()
	db := storedJSONDatabase(t)
	ctx := t.Context()
	_, err := db.Exec(ctx, "INSERT INTO core.pass_booking_admins(owner) VALUES('alice') ON CONFLICT DO NOTHING")
	require.NoError(t, err)
	s := Service{
		DB: db,
		Delivery: delivery.Settings{
			BotID:        999,
			BotInterval:  time.Millisecond,
			ChatInterval: time.Millisecond,
			Fallback:     time.Second,
		},
	}
	for _, captured := range []bool{true, false} {
		operation := "captured"
		if !captured {
			operation = "legacy"
		}
		ref := Reference{Kind: IdentityIntent, Update: 1, Notice: i18n.IdentityUnavailable, Language: "en"}
		chat := int64(101)
		if !captured {
			chat = 202
		}
		obs, enqueueErr := s.Enqueue(
			ctx,
			EnqueueRequest{Chat: chat, Operation: operation, Effect: "notice", Reference: ref, Phase: phaseSend},
		)
		require.NoError(t, enqueueErr)
		i, readErr := Read(ctx, db, 999, obs.Reference, false)
		require.NoError(t, readErr)
		p := PreparedAttempt{Method: "sendMessage", SHA256: strings.Repeat("a", 64)}
		if !captured {
			_, err = db.Exec(ctx, "UPDATE core.delivery_pacing SET not_before='-infinity'")
			require.NoError(t, err)
		}
		in := BeginRequest{Observed: i}
		if captured {
			in.Prepared = &p
		}
		admitted, beginErr := s.Begin(ctx, in)
		require.NoError(t, beginErr)
		require.True(t, admitted.Ready)
		require.NoError(
			t,
			s.Complete(
				ctx,
				CompletionRequest{
					Attempt: admitted.Intent,
					Outcome: delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"},
				},
			),
		)
		r := Resolution{
			IntentKey:      IntentKey{Operation: operation, Effect: "notice"},
			Key:            operation,
			Attempt:        1,
			Disposition:    "confirmed_unsent",
			EvidenceKind:   "complete_sink_proof",
			EvidenceSHA256: strings.Repeat("b", 64),
			PayloadSHA256:  p.SHA256,
			Joined:         true,
			Quiescent:      true,
		}
		out, resolveErr := s.Resolve(ctx, "alice", r)
		if captured {
			require.NoError(t, resolveErr)
			require.Equal(t, delivery.Deferred, out.State)
			require.Equal(t, int64(1), out.Attempt)
			visible, inspectErr := s.Inspect(ctx, "alice", r.IntentKey)
			require.NoError(t, inspectErr)
			require.Equal(t, "confirmed_unsent", visible.Disposition)
		} else {
			require.ErrorIs(t, resolveErr, ErrBinding)
		}
	}
}

func TestResolutionPostgresRuntimePrivileges(t *testing.T) {
	t.Parallel()
	db := storedJSONDatabase(t)
	ctx := t.Context()
	for _, table := range []string{"bot.delivery_attempts", "bot.delivery_resolutions"} {
		var owner string
		require.NoError(
			t,
			db.QueryRow(ctx, "SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid=$1::regclass", table).
				Scan(&owner),
		)
		require.Equal(t, "postgres", owner)
		for _, role := range []string{"zns_app", "zns_api", "zns_runtime", "zns_bot"} {
			allowed := table != "bot.delivery_resolutions" || role != "zns_bot"
			for _, privilege := range []string{"SELECT", "INSERT", "UPDATE", "DELETE"} {
				var granted bool
				require.NoError(
					t,
					db.QueryRow(ctx, "SELECT has_table_privilege($1,$2,$3)", role, table, privilege).Scan(&granted),
				)
				require.Equal(
					t,
					allowed && (privilege == "SELECT" || privilege == "INSERT"),
					granted,
					role+" "+table+" "+privilege,
				)
			}
			column := "method"
			if table == "bot.delivery_resolutions" {
				column = "actor"
			}
			for _, statement := range []string{"UPDATE " + table + " SET " + column + "=" + column + " WHERE FALSE", "DELETE FROM " + table + " WHERE FALSE"} {
				tx, err := db.Begin(ctx)
				require.NoError(t, err)
				_, err = tx.Exec(ctx, "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize())
				require.NoError(t, err)
				_, deniedErr := tx.Exec(ctx, statement)
				require.NoError(t, tx.Rollback(ctx))
				var denied *pgconn.PgError
				require.ErrorAs(t, deniedErr, &denied)
				require.Equal(t, "42501", denied.Code)
			}
		}
	}
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SET LOCAL ROLE zns_bot")
	require.NoError(t, err)
	_, deniedErr := tx.Exec(ctx, "INSERT INTO bot.delivery_resolutions DEFAULT VALUES")
	require.NoError(t, tx.Rollback(ctx))
	var denied *pgconn.PgError
	require.ErrorAs(t, deniedErr, &denied)
	require.Equal(t, "42501", denied.Code)
}
