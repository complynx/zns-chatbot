package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestDeliveryObservationSharedCooldownAgeAndRestart(t *testing.T) {
	t.Parallel()
	db := database(t)
	ref := delivery.Reference{Owner: delivery.Orders, Key: "private-order", Effect: "notice"}
	queueRegister(t, db, ref, delivery.Destination{Chat: "101"}, delivery.Interactive)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.delivery_queue SET enqueued_at=statement_timestamp()-interval '2 minutes'
 WHERE bot_id=$1 AND owner_kind='orders' AND owner_key=$2`,
		queueSettings().BotID,
		ref.Key,
	)
	require.NoError(t, err)
	var original time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT enqueued_at FROM core.delivery_queue WHERE bot_id=$1 AND owner_kind='orders' AND owner_key=$2`, queueSettings().BotID, ref.Key).
			Scan(&original),
	)
	require.True(t, queueBegin(t, db, ref).Ready)
	queueFinish(t, db, ref, delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 60})
	// Repeated enqueue and reconstructed observations must preserve initial age.
	queueRegister(t, db, ref, delivery.Destination{Chat: "101"}, delivery.Interactive)
	var retained time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT enqueued_at FROM core.delivery_queue WHERE bot_id=$1 AND owner_kind='orders' AND owner_key=$2`, queueSettings().BotID, ref.Key).
			Scan(&retained),
	)
	require.Equal(t, original, retained)
	queueRegister(
		t,
		db,
		delivery.Reference{Owner: delivery.Food, Key: "private-food", Effect: "notice"},
		delivery.Destination{Chat: "202"},
		delivery.Background,
	)
	items, err := delivery.QueueObservations(t.Context(), db, queueSettings().BotID)
	require.NoError(t, err)
	require.Len(t, items, 2)
	for _, item := range items {
		require.Equal(t, int64(1), item.Count)
		require.Equal(t, int64(1), item.Delayed, "bot cooldown affects a different owner and chat")
		require.InDelta(t, 60, item.NextAttemptSeconds, 3)
		if item.Owner == delivery.Orders {
			require.GreaterOrEqual(t, item.OldestAgeSeconds, float64(120))
		}
	}
	// No mutable collector state: each call reads the persisted scheduling state.
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.delivery_queue SET enqueued_at=NULL WHERE bot_id=$1 AND owner_kind='food'`,
		queueSettings().BotID,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.delivery_pacing SET pause_reason='telegram_invalid_cooldown',not_before='infinity' WHERE bot_id=$1 AND chat=''`,
		queueSettings().BotID,
	)
	require.NoError(t, err)
	items, err = delivery.QueueObservations(t.Context(), db, queueSettings().BotID)
	require.NoError(t, err)
	for _, item := range items {
		require.Equal(t, int64(1), item.Paused)
		require.Equal(t, int64(1), item.UnboundedDeadline)
		require.Zero(t, item.NextAttemptSeconds)
		if item.Owner == delivery.Food {
			require.Equal(t, int64(1), item.UnknownAge)
			require.Zero(t, item.OldestAgeSeconds)
		}
	}
	items, err = delivery.QueueObservations(t.Context(), db, queueSettings().BotID+1)
	require.NoError(t, err)
	require.Empty(t, items, "observations are scoped to the configured bot")
}

func TestDeliveryObservationIncludesBlockedFollowersAndExcludesTerminal(t *testing.T) {
	t.Parallel()
	db := database(t)
	first := delivery.Reference{Owner: delivery.Orders, Key: "head", Effect: "notice"}
	second := delivery.Reference{Owner: delivery.Orders, Key: "follower", Effect: "notice"}
	queueRegister(t, db, first, delivery.Destination{Chat: "101"}, delivery.Interactive)
	queueRegister(t, db, second, delivery.Destination{Chat: "101"}, delivery.Interactive)
	require.True(t, queueBegin(t, db, first).Ready)
	queueFinish(t, db, first, delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"})
	items, err := delivery.QueueObservations(t.Context(), db, queueSettings().BotID)
	require.NoError(t, err)
	require.Len(t, items, 2, "uncertain head and blocked pending follower stay visible")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.delivery_queue SET state='failed' WHERE bot_id=$1 AND owner_kind='orders' AND owner_key='head'`,
		queueSettings().BotID,
	)
	require.NoError(t, err)
	items, err = delivery.QueueObservations(t.Context(), db, queueSettings().BotID)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, delivery.Deferred, items[0].State)
	require.Equal(t, int64(1), items[0].Count)
}
