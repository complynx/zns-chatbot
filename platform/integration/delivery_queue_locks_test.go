package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestDeliveryQueueLockReferencesConcurrentOrder(t *testing.T) {
	t.Parallel()
	db := database(t)
	one := delivery.Reference{Owner: delivery.Admin, Key: "one", Effect: "send"}
	two := delivery.Reference{Owner: delivery.Admin, Key: "two", Effect: "send"}
	queueRegister(t, db, one, delivery.Destination{Chat: "101"}, delivery.Background)
	queueRegister(t, db, two, delivery.Destination{Chat: "202"}, delivery.Background)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, refs := range [][]delivery.Reference{{one, two, one}, {two, one, two}} {
		go func() {
			tx, err := db.Begin(ctx)
			if err != nil {
				results <- err
				return
			}
			defer func() { _ = tx.Rollback(ctx) }()
			<-start
			if err = delivery.LockReferences(ctx, tx, queueSettings().BotID, refs); err == nil {
				err = tx.Commit(ctx)
			}
			results <- err
		}()
	}
	close(start)
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	queueTransaction(t, db, func(tx pgx.Tx) {
		entry, err := delivery.ReadReference(t.Context(), tx, queueSettings().BotID, one)
		require.NoError(t, err)
		require.Equal(t, delivery.Deferred, entry.State)
		require.ErrorIs(
			t,
			delivery.LockReferences(
				t.Context(),
				tx,
				queueSettings().BotID,
				[]delivery.Reference{{Owner: delivery.Admin, Key: "missing", Effect: "send"}},
			),
			delivery.ErrQueueReference,
		)
	})
}

func TestDeliveryQueueRegisterBatchValidationAndOrder(t *testing.T) {
	t.Parallel()
	db := database(t)
	first := delivery.Registration{
		Reference:   delivery.Reference{Owner: delivery.Admin, Key: "first", Effect: "send"},
		Destination: delivery.Destination{Chat: "202", Thread: 1},
		Class:       delivery.Background,
	}
	second := delivery.Registration{
		Reference:   delivery.Reference{Owner: delivery.Announcement, Key: "second", Effect: "send"},
		Destination: delivery.Destination{Chat: "202", Thread: 2},
		Class:       delivery.Background,
	}
	other := delivery.Registration{
		Reference:   delivery.Reference{Owner: delivery.Passes, Key: "other", Effect: "send"},
		Destination: delivery.Destination{Chat: "101"},
		Class:       delivery.Background,
	}
	invalid := other
	invalid.Destination.Chat = "@unresolved"
	queueTransaction(t, db, func(tx pgx.Tx) {
		require.NoError(t, delivery.RegisterBatch(t.Context(), tx, 0, nil))
		require.ErrorIs(
			t,
			delivery.RegisterBatch(t.Context(), tx, queueSettings().BotID, []delivery.Registration{first, invalid}),
			delivery.ErrQueueReference,
		)
		_, err := delivery.ReadReference(t.Context(), tx, queueSettings().BotID, first.Reference)
		require.ErrorIs(t, err, delivery.ErrQueueReference)
	})
	requests := []delivery.Registration{first, other, second}
	queueTransaction(t, db, func(tx pgx.Tx) {
		require.NoError(t, delivery.RegisterBatch(t.Context(), tx, queueSettings().BotID, requests))
		require.Equal(t, []delivery.Registration{first, other, second}, requests)
		one, err := delivery.ReadReference(t.Context(), tx, queueSettings().BotID, first.Reference)
		require.NoError(t, err)
		two, err := delivery.ReadReference(t.Context(), tx, queueSettings().BotID, second.Reference)
		require.NoError(t, err)
		require.Less(
			t,
			one.Sequence,
			two.Sequence,
			"cross-domain effects preserve caller order in one chat across topics",
		)
	})
	require.False(t, queueBegin(t, db, second.Reference).Ready)
}
