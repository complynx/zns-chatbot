package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestHistoryReadUsesResultingGeneration(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"page", "selected", "window", "batch", "summary"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			db, _ := bookingFixture(t)
			history := conversation.Service{DB: db}
			ctx := t.Context()
			require.NoError(t, history.AppendOriginal(ctx, "alice", "source", "user", "private original"))
			require.NoError(t, history.AppendDerived(ctx, "bob", "bad", "dependent body", 0, lazyHistorySource(0)))
			bad := lazyHistoryID(t, history, "bob", "bad")
			if mode == "summary" {
				window, err := history.Window(ctx, "bob", 1)
				require.NoError(t, err)
				require.NoError(
					t,
					history.CommitSummary(ctx, "bob", window.Summary.Version, []int64{bad}, "dependent summary"),
				)
			}
			require.NoError(
				t,
				history.AppendDerived(ctx, "bob", "good", "independent derived body", 0, []readsource.Authority{}),
			)
			good := lazyHistoryID(t, history, "bob", "good")
			if mode != "batch" {
				require.NoError(
					t,
					history.AppendDerived(
						ctx,
						"bob",
						"long",
						strings.Repeat("long derived ", conversation.MaxTextBytes),
						0,
						[]readsource.Authority{},
					),
				)
			}
			require.NoError(t, history.AppendOriginal(ctx, "bob", "original", "user", "preserved original"))
			original := lazyHistoryID(t, history, "bob", "original")
			require.NoError(t, history.DeleteContent(ctx, "alice", lazyHistoryID(t, history, "alice", "source")))
			for range 2 {
				events := readGenerationEvents(t, history, mode, bad, good, original)
				require.NotEmpty(t, events)
				for _, event := range events {
					if event.ID == original {
						require.False(t, event.Omitted)
						require.Equal(t, "preserved original", event.Text)
						continue
					}
					require.True(
						t,
						event.Omitted,
						"mode=%s event=%d must not expose old-generation derived text",
						mode,
						event.ID,
					)
					require.Empty(t, event.Text)
					require.Empty(t, event.ReadAuthorities)
					require.False(t, event.HasFullText)
				}
				generation, err := history.Generation(ctx, "bob")
				require.NoError(t, err)
				require.Equal(t, int64(1), generation, "one revocation, not another epoch on lazy cleanup")
			}
		})
	}
}

func readGenerationEvents(
	t *testing.T,
	history conversation.Service,
	mode string,
	bad, good, original int64,
) []conversation.Event {
	t.Helper()
	ctx := t.Context()
	switch mode {
	case "page":
		page, err := history.Read(ctx, "bob", conversation.Query{Limit: 10})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Generation)
		return page.Events
	case "selected":
		page, err := history.ReadSelected(ctx, "bob", []int64{bad, good, original})
		require.NoError(t, err)
		require.Equal(t, int64(1), page.Generation)
		return page.Events
	case "batch":
		events, err := history.SummaryBatch(ctx, "bob", original+1)
		require.NoError(t, err)
		return events
	default:
		count := 10
		if mode == "summary" {
			count = 3
		}
		window, err := history.Window(ctx, "bob", count)
		require.NoError(t, err)
		require.Equal(t, int64(1), window.Generation)
		require.Empty(t, window.Summary.Text)
		require.Empty(t, window.Summary.ReadAuthorities)
		return window.Recent
	}
}
