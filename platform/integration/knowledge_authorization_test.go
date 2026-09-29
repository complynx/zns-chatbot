package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestKnowledgeAuthorizationAtEveryModelBoundary(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"knowledge", "history", "retry", "database_failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := knowledgeAuthorizationFixture(t)
			calls := 0
			f.b.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
				calls++
				switch calls {
				case 1:
					return agent.Plan{View: agent.KnowledgeView, KnowledgeAction: &agent.KnowledgeProposal{
						Name: agent.KnowledgeProposals, ReviewQueue: true,
					}}, nil
				case 2:
					require.NotEmpty(t, input.Knowledge.Reads[0].Proposals)
					revokeKnowledgeReview(ctx, t, f, scenario)
					if scenario == "retry" {
						return agent.Plan{}, context.Canceled
					}
					return knowledgeAuthorizationNextRead(scenario), nil
				default:
					require.NotEqual(t, "database_failure", scenario, "authorization errors must stop model invocation")
					assertKnowledgeReviewHidden(t, input)
					if scenario == "retry" && calls == 3 {
						return knowledgeAuthorizationNextRead("knowledge"), nil
					}
					return agent.Plan{View: "workflow", Text: "The allowed read completed."}, nil
				}
			})
			update := message(998, identity.BobTelegramID, "Read pending proposals and supporting context")
			if scenario == "retry" {
				require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
			}
			handle(t, f.b, update)
			expected := map[string]int{"knowledge": 3, "history": 3, "retry": 4, "database_failure": 2}
			assert.Equal(t, expected[scenario], calls)
		})
	}
}

func TestProviderRefreshesKnowledgeAndRegistrationAfterSelection(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"revoke", "database_failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			seedProviderPrivateReads(t, f)
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				assert.NoError(t, err)
				text := `{"text":"Authorized answer","view":"workflow","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null}`
				if calls.Add(1) == 1 {
					assert.Contains(t, string(body), "Private pending proposal")
					assert.Contains(t, string(body), "PRIVATE REGISTRATION MARKER")
					query := `DELETE FROM core.knowledge_permissions WHERE actor='bob'; DELETE FROM core.pass_booking_admins WHERE owner='bob'`
					if scenario == "database_failure" {
						query = `ALTER TABLE core.knowledge_permissions RENAME TO unavailable_provider_permissions`
					}
					_, err = f.db.Exec(r.Context(), query)
					assert.NoError(t, err)
					text = `{"skills":["knowledge","registration"],"reply_language":"en"}`
				} else {
					assert.NotContains(t, string(body), "Private pending proposal")
					assert.NotContains(t, string(body), "PRIVATE REGISTRATION MARKER")
				}
				encoded, err := json.Marshal(text)
				assert.NoError(t, err)
				_, err = w.Write(
					[]byte(
						`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":` + string(
							encoded,
						) + `}]}]}`,
					),
				)
				assert.NoError(t, err)
			}))
			defer server.Close()
			f.b.Model = agent.OpenAI{Key: "synthetic", HTTP: server.Client(), BaseURL: server.URL}
			handle(t, f.b, message(997, identity.BobTelegramID, "Review the available context"))
			expected := int32(2)
			if scenario == "database_failure" {
				expected = 1
			}
			assert.Equal(t, expected, calls.Load())
		})
	}
}

func seedProviderPrivateReads(t *testing.T, f *fixture) {
	t.Helper()
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
	)
	require.NoError(t, err)
	target, err := f.b.API.PassAdminTarget(t.Context(), "bob", "dance", identity.AliceTelegramID)
	require.NoError(t, err)
	target.Name = "PRIVATE REGISTRATION MARKER"
	registration := []agent.RegistrationReadResult{
		{
			Request: agent.RegistrationProposal{
				Name:   "read",
				Event:  "dance",
				View:   agent.RegistrationAdminTarget,
				Target: "101",
			},
			AdminTarget: &target,
		},
	}
	reads := []agent.KnowledgeReadResult{{
		Request:   agent.KnowledgeProposal{Name: agent.KnowledgeProposals, ReviewQueue: true},
		Proposals: []knowledge.Proposal{{Text: "Private pending proposal"}},
	}}
	_, err = f.db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES('bob',997,'knowledge_reads',$1),('bob',997,'registration_reads',$2)`, reads, registration)
	require.NoError(t, err)
}

func TestKnowledgeReviewOutsideDiscoveryLimit(t *testing.T) {
	t.Parallel()
	f := knowledgeAuthorizationFixture(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at)
SELECT 'review-event-'||n,now()+n*interval '1 day' FROM generate_series(1,25) n;
INSERT INTO core.knowledge_scopes(scope,event_id) VALUES('review-event-1','review-event-1');
INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('review-event-1','bob','review');
UPDATE core.knowledge_proposals SET scope='review-event-1'`)
	require.NoError(t, err)
	scopes, err := f.b.API.KnowledgeScopes(t.Context(), "bob")
	require.NoError(t, err)
	require.Len(t, scopes, knowledge.MaxResults)
	for _, scope := range scopes {
		require.NotEqual(t, "review-event-1", scope.Event)
	}
	calls := 0
	f.b.Model = avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
		calls++
		switch calls {
		case 1:
			return agent.Plan{View: agent.KnowledgeView, KnowledgeAction: &agent.KnowledgeProposal{
				Name: agent.KnowledgeProposals, Event: "review-event-1", ReviewQueue: true,
			}}, nil
		case 2:
			require.NotEmpty(t, input.Knowledge.Reads[0].Proposals)
			return knowledgeAuthorizationNextRead("history"), nil
		case 3:
			require.NotEmpty(t, input.Knowledge.Reads[0].Proposals)
			revokeKnowledgeReview(ctx, t, f, "knowledge")
			return knowledgeAuthorizationNextRead("knowledge"), nil
		default:
			assertKnowledgeReviewHidden(t, input)
			return agent.Plan{View: "workflow", Text: "The allowed read completed."}, nil
		}
	})
	handle(t, f.b, message(999, identity.BobTelegramID, "Read the event review queue and context"))
	assert.Equal(t, 4, calls)
}

func knowledgeAuthorizationFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.knowledge_permissions(scope,actor,permission)
VALUES('','bob','review'),('','bob','curate');
INSERT INTO core.knowledge_proposals(scope,owner,topic,fact_key,body,fact_version,state)
VALUES('','alice','travel','private-review','Private pending proposal',0,'pending_review')`)
	require.NoError(t, err)
	return f
}

func revokeKnowledgeReview(ctx context.Context, t *testing.T, f *fixture, scenario string) {
	t.Helper()
	query := `DELETE FROM core.knowledge_permissions WHERE actor='bob'`
	if scenario == "database_failure" {
		query = `ALTER TABLE core.knowledge_permissions RENAME TO unavailable_knowledge_permissions`
	}
	_, err := f.db.Exec(ctx, query)
	require.NoError(t, err)
}

func knowledgeAuthorizationNextRead(scenario string) agent.Plan {
	if scenario == "knowledge" {
		return agent.Plan{
			View:            agent.KnowledgeView,
			KnowledgeAction: &agent.KnowledgeProposal{Name: agent.KnowledgeRead},
		}
	}
	return agent.Plan{View: "workflow", HistoryAction: &agent.HistoryProposal{}}
}

func assertKnowledgeReviewHidden(t *testing.T, input agent.Input) {
	t.Helper()
	require.NotEmpty(t, input.Knowledge.Reads)
	read := input.Knowledge.Reads[0]
	assert.Empty(t, read.Proposals)
	assert.Equal(t, "forbidden", read.Error)
	assert.Equal(t, agent.MaxKnowledgeReads-len(input.Knowledge.Reads), input.Knowledge.Remaining)
	for _, scope := range input.Knowledge.Scopes {
		assert.False(t, scope.CanReview)
		assert.False(t, scope.CanCurate)
	}
}
