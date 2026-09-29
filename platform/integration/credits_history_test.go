package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsHistoryPagesStayOwnerBound(t *testing.T) {
	t.Parallel()
	service := credits.Service{DB: database(t)}
	for range 23 {
		require.NoError(t, service.Reserve(t.Context(), creditAttempt("alice")))
	}
	require.NoError(t, service.Reserve(t.Context(), creditAttempt("bob")))
	first, err := service.History(t.Context(), "alice", "alice", "")
	require.NoError(t, err)
	require.Len(t, first.Items, 20)
	require.True(t, first.More)
	second, err := service.History(t.Context(), "alice", "alice", first.NextCursor)
	require.NoError(t, err)
	require.Len(t, second.Items, 3)
	require.False(t, second.More)
	seen := map[string]bool{}
	for _, item := range first.Items {
		seen[item.ID] = true
	}
	for _, item := range second.Items {
		require.False(t, seen[item.ID])
	}
	_, err = service.History(t.Context(), "bob", "alice", first.NextCursor)
	requireCode(t, err, "forbidden")
	_, err = service.History(t.Context(), "bob", "bob", first.NextCursor)
	requireCode(t, err, "read_cursor_invalid")
}
