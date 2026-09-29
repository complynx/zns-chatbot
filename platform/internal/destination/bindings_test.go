package destination_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/destination"
)

type resolverFunc func(context.Context, string) (int64, error)

func (f resolverFunc) ResolveChat(ctx context.Context, alias string) (int64, error) {
	return f(ctx, alias)
}
func TestBindingsRefreshExpiryAndNumeric(t *testing.T) {
	t.Parallel()
	b := &destination.Bindings{}
	calls := 0
	resolver := resolverFunc(func(ctx context.Context, _ string) (int64, error) {
		calls++
		_, bounded := ctx.Deadline()
		require.True(t, bounded)
		return -100123, nil
	})
	require.NoError(t, b.Refresh(t.Context(), resolver, []string{"@firstchat", "@firstchat", "101"}, time.Minute))
	require.Equal(t, 1, calls)
	chat, err := b.Lookup("@firstchat")
	require.NoError(t, err)
	require.Equal(t, "-100123", chat)
	require.NoError(t, b.Refresh(t.Context(), resolver, []string{"@firstchat"}, time.Nanosecond))
	require.Eventually(t, func() bool {
		_, lookupErr := b.Lookup("@firstchat")
		return errors.Is(lookupErr, destination.ErrUnavailable)
	}, time.Second, time.Millisecond)
	chat, err = b.Lookup("101")
	require.NoError(t, err)
	require.Equal(t, "101", chat)
	require.Error(t, b.Refresh(t.Context(), resolver, []string{"@firstchat"}, time.Hour))
}
func TestBindingsPartialFailureAndUnextendedExpiry(t *testing.T) {
	t.Parallel()
	b := &destination.Bindings{}
	require.NoError(
		t,
		b.Refresh(
			t.Context(),
			resolverFunc(func(context.Context, string) (int64, error) { return 101, nil }),
			[]string{"@brokenchat", "@removedchat"},
			time.Second,
		),
	)
	resolver := resolverFunc(func(_ context.Context, alias string) (int64, error) {
		if alias == "@brokenchat" {
			return 0, errors.New("offline")
		}
		return 202, nil
	})
	require.ErrorIs(
		t,
		b.Refresh(t.Context(), resolver, []string{"@brokenchat", "@goodchat"}, time.Minute),
		destination.ErrUnavailable,
	)
	chat, err := b.Lookup("@goodchat")
	require.NoError(t, err)
	require.Equal(t, "202", chat)
	_, err = b.Lookup("@removedchat")
	require.ErrorIs(t, err, destination.ErrUnavailable)
	require.Eventually(t, func() bool {
		_, lookupErr := b.Lookup("@brokenchat")
		return errors.Is(lookupErr, destination.ErrUnavailable)
	}, 2*time.Second, time.Millisecond)
	chat, err = b.Lookup("@goodchat")
	require.NoError(t, err)
	require.Equal(t, "202", chat)
}

func TestBindingsStalledAliasDoesNotStarveHealthyAlias(t *testing.T) {
	t.Parallel()
	b := &destination.Bindings{}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	resolver := resolverFunc(func(ctx context.Context, alias string) (int64, error) {
		if alias == "@stalledchat" {
			<-ctx.Done()
			return 0, ctx.Err()
		}
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		return 303, nil
	})
	require.ErrorIs(
		t,
		b.Refresh(ctx, resolver, []string{"@stalledchat", "@healthychat"}, time.Minute),
		destination.ErrUnavailable,
	)
	chat, err := b.Lookup("@healthychat")
	require.NoError(t, err)
	require.Equal(t, "303", chat)
	_, err = b.Lookup("@stalledchat")
	require.ErrorIs(t, err, destination.ErrUnavailable)
}
