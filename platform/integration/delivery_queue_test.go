package integration_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func queueSettings() delivery.Settings {
	return delivery.Settings{
		BotID:        4242,
		BotInterval:  time.Nanosecond,
		ChatInterval: time.Nanosecond,
		Fallback:     time.Second,
	}
}

func queueTransaction(t *testing.T, db *pgxpool.Pool, action func(pgx.Tx)) {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	action(tx)
	require.NoError(t, tx.Commit(t.Context()))
}

func queueRegister(
	t *testing.T,
	db *pgxpool.Pool,
	ref delivery.Reference,
	destination delivery.Destination,
	class delivery.Class,
) delivery.Entry {
	t.Helper()
	var entry delivery.Entry
	queueTransaction(t, db, func(tx pgx.Tx) {
		var err error
		entry, err = delivery.Register(t.Context(), tx, queueSettings().BotID, ref, destination, class)
		require.NoError(t, err)
	})
	return entry
}

func queueCandidates(t *testing.T, db *pgxpool.Pool) []delivery.Entry {
	t.Helper()
	var entries []delivery.Entry
	queueTransaction(t, db, func(tx pgx.Tx) {
		var err error
		entries, err = delivery.Candidates(t.Context(), tx, queueSettings().BotID, 20)
		require.NoError(t, err)
	})
	return entries
}

func queueBegin(t *testing.T, db *pgxpool.Pool, ref delivery.Reference) delivery.Admission {
	t.Helper()
	var gate delivery.Admission
	queueTransaction(t, db, func(tx pgx.Tx) {
		var err error
		gate, err = delivery.Begin(t.Context(), tx, queueSettings(), ref)
		require.NoError(t, err)
	})
	return gate
}

func queueFinish(t *testing.T, db *pgxpool.Pool, ref delivery.Reference, outcome delivery.Outcome) {
	t.Helper()
	queueTransaction(t, db, func(tx pgx.Tx) {
		actual, _, err := delivery.Finish(t.Context(), tx, queueSettings(), ref, outcome)
		require.NoError(t, err)
		require.Equal(t, outcome.Kind, actual.Kind)
	})
}

func TestDeliveryQueueCrossDomainHeadAndLateReceipt(t *testing.T) {
	t.Parallel()
	db := database(t)
	first := delivery.Reference{Owner: delivery.Orders, Key: "one", Effect: "notice"}
	second := delivery.Reference{Owner: delivery.Food, Key: "two", Effect: "notice"}
	other := delivery.Reference{Owner: delivery.Bot, Key: "three", Effect: "reply"}
	queueRegister(t, db, first, delivery.Destination{Chat: "101", Thread: 1}, delivery.Background)
	queueRegister(t, db, second, delivery.Destination{Chat: "101", Thread: 2}, delivery.Interactive)
	queueRegister(t, db, other, delivery.Destination{Chat: "202"}, delivery.Interactive)
	require.False(t, queueBegin(t, db, second).Ready, "topics do not bypass the chat head")
	require.True(t, queueBegin(t, db, first).Ready)
	queueTransaction(t, db, func(tx pgx.Tx) {
		require.NoError(
			t,
			delivery.Project(t.Context(), tx, queueSettings().BotID, first, delivery.Uncertain, time.Time{}),
		)
	})
	require.Equal(t, []delivery.Reference{other}, queueReferences(queueCandidates(t, db)))
	require.False(t, queueBegin(t, db, second).Ready)
	queueFinish(t, db, first, delivery.Outcome{Kind: delivery.Succeeded, MessageID: 55})
	require.True(t, queueBegin(t, db, second).Ready, "late known receipt releases exactly its lane head")
}

func queueReferences(entries []delivery.Entry) []delivery.Reference {
	refs := make([]delivery.Reference, 0, len(entries))
	for _, entry := range entries {
		refs = append(refs, entry.Reference)
	}
	return refs
}

func TestDeliveryQueueBindingAndTerminalReplay(t *testing.T) {
	t.Parallel()
	db := database(t)
	ref := delivery.Reference{Owner: delivery.Bot, Key: "update-1", Effect: "chunk-0"}
	destination := delivery.Destination{Chat: "-100101"}
	first := queueRegister(t, db, ref, destination, delivery.Interactive)
	require.Equal(t, first, queueRegister(t, db, ref, destination, delivery.Interactive))
	queueTransaction(t, db, func(tx pgx.Tx) {
		require.NoError(
			t,
			delivery.Project(t.Context(), tx, queueSettings().BotID, ref, delivery.Cancelled, time.Time{}),
		)
	})
	replayed := queueRegister(t, db, ref, destination, delivery.Interactive)
	require.Equal(t, first.Sequence, replayed.Sequence)
	require.Equal(t, delivery.Cancelled, replayed.State)
	require.Empty(t, queueCandidates(t, db))
	for _, chat := range []string{"-100102", "00101", "@alias", "0"} {
		tx, err := db.Begin(t.Context())
		require.NoError(t, err)
		_, err = delivery.Register(
			t.Context(),
			tx,
			queueSettings().BotID,
			ref,
			delivery.Destination{Chat: chat},
			delivery.Interactive,
		)
		require.Error(t, err)
		require.NoError(t, tx.Rollback(t.Context()))
	}
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	err = delivery.Project(t.Context(), tx, queueSettings().BotID, ref, delivery.Deferred, time.Time{})
	require.ErrorIs(t, err, delivery.ErrQueueState)
	require.NoError(t, tx.Rollback(t.Context()))
	tx, err = db.Begin(t.Context())
	require.NoError(t, err)
	err = delivery.Project(t.Context(), tx, queueSettings().BotID,
		delivery.Reference{Owner: delivery.Bot, Key: "missing", Effect: "reply"}, delivery.Cancelled, time.Time{})
	require.ErrorIs(t, err, delivery.ErrQueueReference)
	require.NoError(t, tx.Rollback(t.Context()))
}

func TestDeliveryQueueCooldownAndProjectionRollback(t *testing.T) {
	t.Parallel()
	db := database(t)
	ref := delivery.Reference{Owner: delivery.Admin, Key: "first", Effect: "send"}
	other := delivery.Reference{Owner: delivery.Passes, Key: "other", Effect: "send"}
	queueRegister(t, db, ref, delivery.Destination{Chat: "101"}, delivery.Background)
	queueRegister(t, db, other, delivery.Destination{Chat: "202"}, delivery.Background)
	require.True(t, queueBegin(t, db, ref).Ready)
	limited := delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 3600}
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	_, deadline, err := delivery.Finish(t.Context(), tx, queueSettings(), ref, limited)
	require.NoError(t, err)
	require.True(t, deadline.After(time.Now().Add(59*time.Minute)))
	require.NoError(t, tx.Rollback(t.Context()), "owner failure must roll back both projection and cooldown")
	require.Equal(t, []delivery.Reference{other}, queueReferences(queueCandidates(t, db)))
	queueFinish(t, db, ref, limited)
	require.Empty(t, queueCandidates(t, db), "429 bot cooldown must block a different domain and chat")
	require.False(t, queueBegin(t, db, other).Ready)
}

func TestDeliveryQueueFairCandidatesPersistAcrossTransactions(t *testing.T) {
	t.Parallel()
	db := database(t)
	for _, lane := range []struct {
		chat  string
		class delivery.Class
	}{
		{"101", delivery.Interactive}, {"102", delivery.Interactive}, {"202", delivery.Background},
	} {
		for index := range 4 {
			ref := delivery.Reference{Owner: delivery.Bot, Key: lane.chat, Effect: strconv.Itoa(index)}
			queueRegister(t, db, ref, delivery.Destination{Chat: lane.chat}, lane.class)
		}
	}
	for _, chat := range []string{"101", "102", "101", "202", "102", "101", "102", "202"} {
		entries := queueCandidates(t, db)
		require.NotEmpty(t, entries)
		require.Equal(t, chat, entries[0].Destination.Chat)
		require.True(t, queueBegin(t, db, entries[0].Reference).Ready)
		queueFinish(t, db, entries[0].Reference, delivery.Outcome{Kind: delivery.Succeeded, MessageID: 99})
	}
}

func TestDeliveryQueueConcurrentEnqueueWaitsForCommit(t *testing.T) {
	t.Parallel()
	db := database(t)
	firstTx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = firstTx.Rollback(t.Context()) }()
	first := delivery.Reference{Owner: delivery.Orders, Key: "first", Effect: "send"}
	entry, err := delivery.Register(t.Context(), firstTx, queueSettings().BotID, first,
		delivery.Destination{Chat: "101"}, delivery.Background)
	require.NoError(t, err)
	require.Equal(t, int64(1), entry.Sequence)
	secondTx, err := db.Begin(t.Context())
	require.NoError(t, err)

	var pid int
	require.NoError(t, secondTx.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
	type result struct {
		entry delivery.Entry
		err   error
	}
	completed := make(chan result, 1)
	go func() {
		defer func() { _ = secondTx.Rollback(t.Context()) }()
		second := delivery.Reference{Owner: delivery.Food, Key: "second", Effect: "send"}
		item, registerErr := delivery.Register(t.Context(), secondTx, queueSettings().BotID, second,
			delivery.Destination{Chat: "101"}, delivery.Interactive)
		if registerErr == nil {
			registerErr = secondTx.Commit(t.Context())
		}
		completed <- result{entry: item, err: registerErr}
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		queryErr := db.QueryRow(t.Context(), "SELECT cardinality(pg_blocking_pids($1))>0", pid).Scan(&blocked)
		return queryErr == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	require.Empty(t, queueCandidates(t, db), "later enqueue cannot become visible ahead of the uncommitted head")
	require.NoError(t, firstTx.Commit(t.Context()))
	select {
	case second := <-completed:
		require.NoError(t, second.err)
		require.Equal(t, int64(2), second.entry.Sequence)
	case <-time.After(5 * time.Second):
		t.Fatal("second registration did not finish after lane commit")
	}
	require.Equal(t, []delivery.Reference{first}, queueReferences(queueCandidates(t, db)))
}

func TestDeliveryQueueOwnerRejectionReleasesFollower(t *testing.T) {
	t.Parallel()
	for _, state := range []delivery.Kind{delivery.Deferred, delivery.Paused, delivery.Parked, delivery.Sending, delivery.Uncertain} {
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			db := database(t)
			first := delivery.Reference{Owner: delivery.Bot, Key: "exhausted", Effect: "reply"}
			follower := delivery.Reference{Owner: delivery.Passes, Key: "next", Effect: "notice"}
			destination := delivery.Destination{Chat: "101"}
			entry := queueRegister(t, db, first, destination, delivery.Interactive)
			queueRegister(t, db, follower, destination, delivery.Background)
			if state == delivery.Sending || state == delivery.Uncertain {
				require.True(t, queueBegin(t, db, first).Ready)
			}
			if state != delivery.Sending {
				queueTransaction(t, db, func(tx pgx.Tx) {
					require.NoError(
						t,
						delivery.Project(t.Context(), tx, queueSettings().BotID, first, state, time.Time{}),
					)
				})
			}
			require.False(t, queueBegin(t, db, follower).Ready)
			pacing := queueSchedulingSnapshot(t, db)
			tx, err := db.Begin(t.Context())
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(t.Context()) }()
			require.NoError(
				t,
				delivery.Project(t.Context(), tx, queueSettings().BotID, first, delivery.Rejected, time.Time{}),
			)
			require.NoError(t, tx.Rollback(t.Context()))
			require.Equal(t, state, queueRegister(t, db, first, destination, delivery.Interactive).State)
			require.False(t, queueBegin(t, db, follower).Ready, "rollback keeps the original head")
			queueTransaction(t, db, func(tx pgx.Tx) {
				require.NoError(
					t,
					delivery.Project(t.Context(), tx, queueSettings().BotID, first, delivery.Rejected, time.Time{}),
				)
			})
			replayed := queueRegister(t, db, first, destination, delivery.Interactive)
			require.Equal(t, delivery.Rejected, replayed.State)
			require.Equal(t, entry.Sequence, replayed.Sequence)
			queueTransaction(t, db, func(tx pgx.Tx) {
				require.NoError(
					t,
					delivery.Project(t.Context(), tx, queueSettings().BotID, first, delivery.Rejected, time.Time{}),
				)
			})
			require.Equal(
				t,
				pacing,
				queueSchedulingSnapshot(t, db),
				"owner rejection does not reserve or extend pacing",
			)
			require.Equal(t, []delivery.Reference{follower}, queueReferences(queueCandidates(t, db)))
			queueRejectedGuards(t, db, first)
			require.True(t, queueBegin(t, db, follower).Ready, "committed owner rejection releases its lane")
		})
	}
}

func queueSchedulingSnapshot(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var snapshot string
	err := db.QueryRow(t.Context(), `SELECT jsonb_build_object(
		'pacing', (SELECT jsonb_agg(to_jsonb(p) ORDER BY p.chat) FROM core.delivery_pacing p WHERE bot_id=$1),
		'fairness', (SELECT to_jsonb(f) FROM core.delivery_fairness f WHERE bot_id=$1),
		'lanes', (SELECT jsonb_agg(to_jsonb(l) ORDER BY l.chat) FROM core.delivery_lanes l WHERE bot_id=$1)
	)::text`, queueSettings().BotID).Scan(&snapshot)
	require.NoError(t, err)
	return snapshot
}

func queueRejectedGuards(t *testing.T, db *pgxpool.Pool, ref delivery.Reference) {
	t.Helper()
	for _, state := range []delivery.Kind{delivery.Deferred, delivery.Cancelled, delivery.Sending, delivery.Succeeded} {
		tx, err := db.Begin(t.Context())
		require.NoError(t, err)
		require.ErrorIs(
			t,
			delivery.Project(t.Context(), tx, queueSettings().BotID, ref, state, time.Time{}),
			delivery.ErrQueueState,
		)
		require.NoError(t, tx.Rollback(t.Context()))
	}
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	_, _, err = delivery.Finish(
		t.Context(),
		tx,
		queueSettings(),
		ref,
		delivery.Outcome{Kind: delivery.Succeeded, MessageID: 55},
	)
	require.ErrorIs(t, err, delivery.ErrQueueState, "a late receipt cannot resurrect a definitively rejected attempt")
	require.NoError(t, tx.Rollback(t.Context()))
}
