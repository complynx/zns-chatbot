package bot

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func dispatchTestEntry(key string) delivery.Entry {
	return delivery.Entry{Reference: delivery.Reference{Owner: delivery.Bot, Key: key, Effect: botPhaseSend}}
}

func TestDeliveryDispatcherReselectsAfterEachAttempt(t *testing.T) {
	t.Parallel()
	a, b, c := dispatchTestEntry("a"), dispatchTestEntry("b"), dispatchTestEntry("c")
	var sent []string
	queries := 0
	err := dispatchDeliveryPass(t.Context(), func(context.Context) ([]delivery.Entry, error) {
		queries++
		switch len(sent) {
		case 0:
			return []delivery.Entry{a, b}, nil
		case 1:
			// A committed grant changed fairness, putting a new head first.
			return []delivery.Entry{c, b}, nil
		case 2:
			return []delivery.Entry{b}, nil
		default:
			return nil, nil
		}
	}, func(_ context.Context, ref delivery.Reference) error {
		sent = append(sent, ref.Key)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, []string{"a", "c", "b"}, sent)
	require.Equal(t, len(sent)+1, queries)
}

func TestDeliveryDispatcherFailureDoesNotBlockOtherHead(t *testing.T) {
	t.Parallel()
	failed, healthy := dispatchTestEntry("failed"), dispatchTestEntry("healthy")
	failure := errors.New("owner temporarily unavailable")
	var sent []string
	err := dispatchDeliveryPass(t.Context(), func(context.Context) ([]delivery.Entry, error) {
		// Even a stale advisory snapshot must not spin on one failing head.
		return []delivery.Entry{failed, healthy}, nil
	}, func(_ context.Context, ref delivery.Reference) error {
		sent = append(sent, ref.Key)
		if ref == failed.Reference {
			return failure
		}
		return nil
	})
	require.ErrorIs(t, err, failure)
	require.Equal(t, []string{"failed", "healthy"}, sent)
}

func TestDeliveryDispatcherCancellationStopsNextSelection(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queries := 0
	err := dispatchDeliveryPass(ctx, func(context.Context) ([]delivery.Entry, error) {
		queries++
		return []delivery.Entry{dispatchTestEntry("first"), dispatchTestEntry("second")}, nil
	}, func(context.Context, delivery.Reference) error {
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, queries)
}

func TestDeliveryDispatcherPassIsBounded(t *testing.T) {
	t.Parallel()
	attempts := 0
	err := dispatchDeliveryPass(t.Context(), func(context.Context) ([]delivery.Entry, error) {
		return []delivery.Entry{dispatchTestEntry(strconv.Itoa(attempts))}, nil
	}, func(context.Context, delivery.Reference) error {
		attempts++
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, deliveryPassLimit, attempts)
}

func TestDeliveryDispatcherSelectionErrorDoesNotDispatch(t *testing.T) {
	t.Parallel()
	failure := errors.New("selection failed")
	dispatched := false
	err := dispatchDeliveryPass(t.Context(), func(context.Context) ([]delivery.Entry, error) {
		return nil, failure
	}, func(context.Context, delivery.Reference) error {
		dispatched = true
		return nil
	})
	require.ErrorIs(t, err, failure)
	require.False(t, dispatched)
}

func TestDeliveryDispatcherCancelledSelectionDoesNotDispatch(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	dispatched := false
	err := dispatchDeliveryPass(ctx, func(context.Context) ([]delivery.Entry, error) {
		cancel()
		return []delivery.Entry{dispatchTestEntry("first")}, nil
	}, func(context.Context, delivery.Reference) error {
		dispatched = true
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, dispatched)
}

func TestDeliveryDispatcherRejectsInvalidDomainReference(t *testing.T) {
	t.Parallel()
	for _, ref := range []delivery.Reference{
		{Owner: delivery.Orders, Key: "0", Effect: botPhaseSend},
		{Owner: delivery.Orders, Key: "-1", Effect: botPhaseSend},
		{Owner: delivery.Orders, Key: "01", Effect: botPhaseSend},
		{Owner: delivery.Orders, Key: "text", Effect: botPhaseSend},
		{Owner: delivery.Orders, Key: "1", Effect: botRefreshEffect},
		{Owner: delivery.Owner("unregistered"), Key: "1", Effect: botPhaseSend},
	} {
		var b Bot
		require.ErrorIs(t, b.dispatchDelivery(t.Context(), ref), delivery.ErrQueueReference)
	}
}

func TestDeliveryDispatcherRetainsDatabaseFailureAfterProviderAndCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	provider := errors.New("provider unavailable")
	first, second := dispatchTestEntry("provider"), dispatchTestEntry("database")
	var sent []string
	err := dispatchDeliveryPass(ctx, func(context.Context) ([]delivery.Entry, error) {
		return []delivery.Entry{first, second}, nil
	}, func(_ context.Context, ref delivery.Reference) error {
		sent = append(sent, ref.Key)
		if ref == first.Reference {
			return provider
		}
		cancel()
		return &pgconn.PgError{Code: "08006", Message: "private database detail"}
	})
	require.Equal(t, []string{"provider", "database"}, sent)
	require.ErrorIs(t, err, provider)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.True(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), "private database detail")
}

func TestDeliveryDispatcherStopsBeforeHealthyHeadAfterDatabaseFailure(t *testing.T) {
	t.Parallel()
	provider := errors.New("provider unavailable")
	heads := []delivery.Entry{
		dispatchTestEntry("provider"), dispatchTestEntry("database"), dispatchTestEntry("healthy"),
	}
	var sent []string
	err := dispatchDeliveryPass(t.Context(), func(context.Context) ([]delivery.Entry, error) {
		return heads, nil
	}, func(_ context.Context, ref delivery.Reference) error {
		sent = append(sent, ref.Key)
		if ref == heads[0].Reference {
			return provider
		}
		if ref == heads[1].Reference {
			return &pgconn.PgError{Code: "08006"}
		}
		return nil
	})
	require.Equal(t, []string{"provider", "database"}, sent)
	require.ErrorIs(t, err, provider)
	require.ErrorIs(t, err, core.ErrDatabase)
}
