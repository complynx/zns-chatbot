package agenthost_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type historyPolicyFixture struct {
	agenthost.HistoryReadStore

	generation    int64
	revoked       bool
	events        []conversation.Event
	calls         []string
	checked       [][]readsource.Authority
	committed     []int64
	commitVersion int64
	commitText    string
	pageError     error
	limit         int
}

func (f *historyPolicyFixture) ConversationWindow(_ context.Context, _ string, limit int) (conversation.Window, error) {
	f.limit = limit
	return conversation.Window{Generation: f.generation}, nil
}

func (f *historyPolicyFixture) ConversationHistory(
	context.Context,
	string,
	conversation.Query,
) (conversation.Page, error) {
	f.calls = append(f.calls, "page")
	return conversation.Page{}, f.pageError
}

func (f *historyPolicyFixture) HistoryGeneration(context.Context, string) (int64, error) {
	return f.generation, nil
}

func (f *historyPolicyFixture) HistorySummaryBatch(context.Context, string, int64) ([]conversation.Event, error) {
	f.calls = append(f.calls, "batch")
	return f.events, nil
}

func (f *historyPolicyFixture) CommitHistorySummary(
	_ context.Context,
	_ string,
	version int64,
	ids []int64,
	text string,
) error {
	f.committed, f.commitVersion, f.commitText = ids, version, text
	return nil
}

func (f *historyPolicyFixture) ReserveHistorySummary(context.Context, string, int64) error {
	f.calls = append(f.calls, "reserve-summary")
	return nil
}

func (f *historyPolicyFixture) ReserveHistory(context.Context, string, int64) (int, error) {
	f.calls = append(f.calls, "reserve-read")
	return 0, nil
}

func (f *historyPolicyFixture) History(context.Context, string, int64) ([]conversation.Page, error) {
	return nil, nil
}

func (f *historyPolicyFixture) CheckReadAuthorities(_ context.Context, _ string, refs []readsource.Authority) error {
	f.checked = append(f.checked, readsource.CloneAuthorities(refs))
	if f.revoked {
		return &core.ProblemError{Status: http.StatusConflict, Code: "history_stale"}
	}
	return nil
}

type summaryPolicyModel struct {
	agent.Model

	summarize func(context.Context, agent.HistorySummaryInput) (string, error)
}

func (m summaryPolicyModel) SummarizeHistory(ctx context.Context, input agent.HistorySummaryInput) (string, error) {
	return m.summarize(ctx, input)
}

func summarySource(scope string) readsource.Authority {
	return readsource.Authority{
		Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: scope},
	}
}

func TestHistorySummaryGuardUsesExactSelectedEvidence(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"none", "generation", "source"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			previous, selected, excluded := summarySource(
				"previous",
			), summarySource(
				"selected",
			), summarySource(
				"excluded",
			)
			f := &historyPolicyFixture{generation: 5, events: []conversation.Event{
				{ID: 11, Text: "selected private text", ReadAuthorities: []readsource.Authority{selected}},
				{
					ID:              12,
					Text:            strings.Repeat("x", agent.MaxSummaryInputBytes),
					ReadAuthorities: []readsource.Authority{excluded},
				},
			}}
			var providerRequests int
			model := summaryPolicyModel{
				summarize: func(ctx context.Context, input agent.HistorySummaryInput) (string, error) {
					require.Equal(t, "previous private summary", input.Previous)
					require.Len(t, input.Events, 1)
					require.Empty(t, input.Events[0].ReadAuthorities)
					require.NotNil(t, input.BeforeProvider)
					switch change {
					case "generation":
						f.generation++
					case "source":
						f.revoked = true
					}
					// Simulate the actual provider boundary after accounting admission.
					err := input.BeforeProvider(ctx)
					if change != "none" {
						require.ErrorIs(t, err, appclient.ErrReadStale)
						return "", err
					}
					require.NoError(t, err)
					providerRequests++
					return "authorized summary", nil
				},
			}
			reader := agenthost.HistoryReader{Domain: f, Summaries: f, Authority: f, Store: f, Model: model}
			window := conversation.Window{
				Generation: 5,
				Gap:        true,
				BeforeID:   20,
				Summary: conversation.Summary{
					Version:         3,
					Text:            "previous private summary",
					ReadAuthorities: []readsource.Authority{previous},
				},
			}
			input := agent.Input{}
			require.NoError(t, reader.History(t.Context(), "owner", 71, &input, window))
			require.Equal(t, []string{"reserve-summary", "batch"}, f.calls)
			expected, err := readsource.Merge([]readsource.Authority{previous, selected})
			require.NoError(t, err)
			require.Equal(t, expected, f.checked[0])
			for _, checked := range f.checked {
				require.Equal(t, expected, checked)
			}
			if change == "none" {
				require.Equal(t, 1, providerRequests)
				require.Equal(t, []int64{11}, f.committed)
				require.Equal(t, int64(3), f.commitVersion)
				require.Equal(t, "authorized summary", f.commitText)
				require.Equal(t, conversation.DefaultRecent, f.limit)
			} else {
				require.Zero(t, providerRequests)
				require.Empty(t, f.committed)
				require.True(t, input.Conversation.Gap)
			}
		})
	}
}

func TestHistoryReadCancellationKeepsConsumedReservation(t *testing.T) {
	t.Parallel()
	f := &historyPolicyFixture{pageError: context.Canceled}
	reader := agenthost.HistoryReader{Domain: f, Store: f}
	input := agent.Input{Conversation: &agent.HistoryContext{Remaining: 1}}
	require.ErrorIs(t, reader.ReadHistory(t.Context(), "owner", 72, agent.HistoryProposal{}, &input), context.Canceled)
	require.Equal(t, []string{"reserve-read", "page"}, f.calls)
}
