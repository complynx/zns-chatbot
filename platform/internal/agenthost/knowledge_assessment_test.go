package agenthost_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type assessmentAuthority func(context.Context, string, []readsource.Authority) error

func (f assessmentAuthority) CheckReadAuthorities(
	ctx context.Context,
	owner string,
	refs []readsource.Authority,
) error {
	return f(ctx, owner, refs)
}

type assessmentClassifier func(context.Context, agent.KnowledgeAssessmentInput) (agent.KnowledgeAssessment, error)

func (f assessmentClassifier) AssessKnowledge(
	ctx context.Context,
	input agent.KnowledgeAssessmentInput,
) (agent.KnowledgeAssessment, error) {
	return f(ctx, input)
}

func TestKnowledgeAssessorSeparatesDeferredCancellationAndGuard(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"outage", "cancel", "retired", "budget"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			checkKnowledgeAssessorFailure(t, scenario)
		})
	}
}

func checkKnowledgeAssessorFailure(t *testing.T, scenario string) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	checks := 0
	requests := 0
	denied := errors.New("source retired")
	adapter := agenthost.KnowledgeAssessor{
		Authority: assessmentAuthority(func(context.Context, string, []readsource.Authority) error {
			checks++
			if scenario == "retired" && checks > 1 {
				return denied
			}
			return nil
		}),
		Classifier: assessmentClassifier(
			func(ctx context.Context, input agent.KnowledgeAssessmentInput) (agent.KnowledgeAssessment, error) {
				if err := input.BeforeProvider(ctx); err != nil {
					return agent.KnowledgeAssessment{}, err
				}
				requests++
				if scenario == "cancel" {
					cancel()
					return agent.KnowledgeAssessment{}, ctx.Err()
				}
				if scenario == "budget" {
					return agent.KnowledgeAssessment{}, input.BeforeProvider(ctx)
				}
				return agent.KnowledgeAssessment{}, errors.New("synthetic private provider diagnostic")
			},
		),
	}
	outcome, err := adapter.AssessKnowledge(ctx, "alice", knowledge.Proposal{Text: "Synthetic proposal"})
	switch scenario {
	case "cancel":
		require.ErrorIs(t, err, context.Canceled)
	case "retired":
		require.ErrorIs(t, err, denied)
		require.Zero(t, requests)
	default:
		require.NoError(t, err)
		require.Equal(t, interaction.KnowledgeAssessmentDeferred, outcome.Status)
		require.Zero(t, outcome.Verdict)
		want := interaction.KnowledgeDeferredUnavailable
		if scenario == "budget" {
			want = interaction.KnowledgeDeferredBudget
		}
		require.Equal(t, want, outcome.Reason)
		require.Equal(t, 1, requests)
	}
}

type failedAssessmentAccounting struct {
	cancel           context.CancelFunc
	notSent          int
	cleanupCancelled bool
}

func (*failedAssessmentAccounting) RequestTier() string { return "" }
func (r *failedAssessmentAccounting) Reserve(context.Context, credits.Attempt) error {
	if r.cancel != nil {
		r.cancel()
	}
	return nil
}
func (*failedAssessmentAccounting) Dispatch(context.Context, string) error { return nil }
func (*failedAssessmentAccounting) Settle(context.Context, string, credits.Settlement) error {
	return errors.New("unexpected settlement")
}
func (r *failedAssessmentAccounting) NotSent(ctx context.Context, _ string) error {
	r.notSent++
	r.cleanupCancelled = ctx.Err() != nil
	return errors.New("private recorder diagnostic")
}

func TestKnowledgeAssessorPreservesFailedUnsentCleanup(t *testing.T) {
	t.Parallel()
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "retired", true: "cancelled"}[cancelled], func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			recorder := &failedAssessmentAccounting{}
			if cancelled {
				recorder.cancel = cancel
			}
			var sent atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				sent.Add(1)
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			defer server.Close()
			checks := 0
			denied := errors.New("synthetic source retired")
			adapter := agenthost.KnowledgeAssessor{
				Authority: assessmentAuthority(func(context.Context, string, []readsource.Authority) error {
					checks++
					if checks > 1 {
						return denied
					}
					return nil
				}),
				Classifier: agent.OpenAI{
					Key:        "synthetic",
					BaseURL:    server.URL,
					HTTP:       server.Client(),
					Accounting: recorder,
				},
			}
			result, err := adapter.AssessKnowledge(ctx, "alice", knowledge.Proposal{Text: "synthetic assessment"})
			require.Zero(t, result)
			if cancelled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, denied)
			}
			require.ErrorIs(t, err, credits.ErrAccounting)
			require.NotContains(t, err.Error(), "private recorder diagnostic")
			require.Equal(t, 1, recorder.notSent)
			require.False(t, recorder.cleanupCancelled)
			require.Zero(t, sent.Load())
		})
	}
}
