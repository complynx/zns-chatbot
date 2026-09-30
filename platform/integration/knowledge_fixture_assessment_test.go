package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func installKnowledgeAssessmentFixture(
	t *testing.T,
	owner string,
	update int64,
	worthwhile bool,
	steps []any,
) (sandbox.FixtureRemote, *atomic.Int32) {
	t.Helper()
	fake, err := sandbox.New(t.Context(), nil, "synthetic-token")
	require.NoError(t, err)
	var requests atomic.Int32
	handler := fake.Handler()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/lab/model/knowledge-assessment" {
			requests.Add(1)
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	body, err := json.Marshal(map[string]any{
		"owner": owner, "update_id": update, "steps": steps,
		"assessment": map[string]any{
			"expect": agent.KnowledgeAssessmentInput{
				Topic:   "travel",
				FactKey: "fixture",
				Text:    "Synthetic shuttle leaves at noon.",
			},
			"result": agent.KnowledgeAssessment{Worthwhile: worthwhile, Reason: "Synthetic private assessment reason."},
		},
	})
	require.NoError(t, err)
	request := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/lab/model/fixtures",
		bytes.NewReader(body),
	)
	request.Header.Set("X-Sandbox", "1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	return sandbox.FixtureRemote{URL: server.URL + "/lab/model"}, &requests
}

func TestKnowledgeFixtureAssessmentPrivateDraftAndManualApproval(t *testing.T) {
	t.Parallel()
	for _, worthwhile := range []bool{false, true} {
		t.Run(map[bool]string{false: "filtered", true: "private_draft"}[worthwhile], func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.db.Exec(
				t.Context(),
				`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
			)
			require.NoError(t, err)
			remote, requests := installKnowledgeAssessmentFixture(t, "alice", 971, worthwhile, []any{
				map[string]any{
					"expect": map[string]string{"text": "Suggest the synthetic shuttle fact"},
					"plan": agent.Plan{View: agent.KnowledgeView, KnowledgeAction: &agent.KnowledgeProposal{
						Name:    knowledge.Suggest,
						Topic:   "travel",
						FactKey: "fixture",
						Text:    "Synthetic shuttle leaves at noon.",
					}},
				},
			})
			f.b.Model = remote
			update := message(971, 101, "Suggest the synthetic shuttle fact")
			handleVisible(t, f.b, update)
			require.Equal(t, int32(1), requests.Load())
			proposals, err := f.b.API.KnowledgeProposals(t.Context(), "alice", knowledge.ProposalQuery{})
			require.NoError(t, err)
			require.Len(t, proposals, 1)
			facts, err := f.b.API.Knowledge(t.Context(), "alice", knowledge.Query{})
			require.NoError(t, err)
			require.Empty(t, facts, "a synthetic verdict cannot publish")
			handleVisible(t, f.b, update)
			require.Equal(t, int32(1), requests.Load(), "completed replay must not call the fixture")
			if !worthwhile {
				require.Equal(t, "filtered", proposals[0].State)
				_, err = f.b.Host.SubmitKnowledgeProposal(t.Context(), "alice", proposalSubmission(proposals[0]))
				require.Error(t, err, "negative assessment has no publishable draft")
				return
			}
			submitted := submitKnowledgeCardForAlice(t, f, proposals[0], 972)
			pumpBotDeliveries(t, f.b)
			assert.NotEmpty(t, chatMessages(t, f, 101))
			command := knowledge.Command{Name: knowledge.Review, Key: "fixture-approve", ProposalID: submitted.ID,
				Version: submitted.Version, Decision: "approve"}
			_, err = f.b.API.ExecuteKnowledge(t.Context(), "alice", command)
			require.Error(t, err, "author has no review grant")
			_, err = f.db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
			require.NoError(t, err)
			_, err = f.b.API.ExecuteKnowledge(t.Context(), "bob", command)
			require.Error(t, err, "approval checks current rights")
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review')`,
			)
			require.NoError(t, err)
			_, err = f.b.API.ExecuteKnowledge(t.Context(), "bob", command)
			require.NoError(t, err)
			facts, err = f.b.API.Knowledge(t.Context(), "alice", knowledge.Query{})
			require.NoError(t, err)
			require.Len(t, facts, 1)
			assert.Equal(t, submitted.Text, facts[0].Text)
			assert.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestKnowledgeFixtureAssessmentStoredVerdictSurvivesReconstruction(t *testing.T) {
	t.Parallel()
	f := setup(t)
	remote, requests := installKnowledgeAssessmentFixture(t, "alice", 981, true, nil)
	require.NoError(
		t,
		f.b.Host.ArchiveOriginal(t.Context(), "alice", "tg-user-981", "user", "Synthetic assessment recovery"),
	)
	host := &interruptedKnowledgeHost{Host: f.b.Host, attachmentInterrupted: true}
	coordinator := interaction.KnowledgeCoordinator{Client: f.b.API, Host: host, Store: interaction.Store{DB: f.db},
		Assessor: agenthost.KnowledgeAssessor{Classifier: remote, Authority: f.b.Host}}
	command := knowledge.Command{
		Name:    knowledge.Suggest,
		Topic:   "travel",
		FactKey: "fixture",
		Text:    "Synthetic shuttle leaves at noon.",
	}
	_, err := coordinator.Execute(t.Context(), "alice", 981, command, nil)
	require.EqualError(t, err, "assessment interrupted")
	require.Equal(t, int32(1), requests.Load())
	var verdict agent.KnowledgeAssessment
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=981 AND kind='knowledge_assessment'`).
			Scan(&verdict),
	)
	require.True(t, verdict.Worthwhile)
	restarted := interaction.KnowledgeCoordinator{Client: f.b.API, Host: f.b.Host, Store: interaction.Store{DB: f.db},
		Assessor: agenthost.KnowledgeAssessor{Classifier: remote, Authority: f.b.Host}}
	outcome, err := restarted.Execute(t.Context(), "alice", 981, command, nil)
	require.NoError(t, err)
	require.Nil(t, outcome.Refusal)
	require.Equal(t, int32(1), requests.Load(), "persisted verdict replay makes zero additional provider requests")
	proposals, err := f.b.API.KnowledgeProposals(t.Context(), "alice", knowledge.ProposalQuery{})
	require.NoError(t, err)
	require.Len(t, proposals, 1)
	assert.Equal(t, knowledge.AwaitingSubmission, proposals[0].State)
}

type fixtureAssessmentAuthority struct {
	f      *fixture
	checks int
}

func (a *fixtureAssessmentAuthority) CheckReadAuthorities(
	ctx context.Context,
	owner string,
	refs []readsource.Authority,
) error {
	a.checks++
	if a.checks == 2 {
		if _, err := a.f.b.API.ExecuteKnowledge(ctx, owner, knowledge.Command{
			Name:    knowledge.MemoDelete,
			Key:     "source-changed",
			FactKey: "fixture-source",
			Version: 1,
		}); err != nil {
			return err
		}
	}
	return a.f.b.Host.CheckReadAuthorities(ctx, owner, refs)
}

func TestKnowledgeFixtureAssessmentRechecksSourceBeforeWire(t *testing.T) {
	t.Parallel()
	f := setup(t)
	remote, requests := installKnowledgeAssessmentFixture(t, "alice", 991, true, nil)
	generation := int64(0)
	_, err := f.b.Host.ExecuteDerivedKnowledge(t.Context(), "alice", knowledge.Command{
		Name: knowledge.MemoSet, Key: "source", FactKey: "fixture-source", Text: "Original synthetic source",
	}, readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}})
	require.NoError(t, err)
	memo, err := f.b.API.Memo(t.Context(), "alice", "fixture-source")
	require.NoError(t, err)
	require.NotEmpty(t, memo.ReadAuthorities)
	authority := &fixtureAssessmentAuthority{f: f}
	assessor := agenthost.KnowledgeAssessor{Classifier: remote, Authority: authority}
	ctx := agent.WithRequestScope(t.Context(), agent.RequestScope{Owner: "alice", UpdateID: 991})
	_, err = assessor.AssessKnowledge(ctx, "alice", knowledge.Proposal{
		Topic:           "travel",
		FactKey:         "fixture",
		Text:            "Synthetic shuttle leaves at noon.",
		ReadAuthorities: memo.ReadAuthorities,
	})
	require.Error(t, err)
	assert.Equal(t, 2, authority.checks)
	assert.Zero(t, requests.Load(), "shared BeforeProvider authority denial must stop HTTP I/O")
	current, err := f.b.API.Memo(t.Context(), "alice", "fixture-source")
	require.NoError(t, err)
	assert.False(t, current.Active, "the source was actually deleted before the second authority check")
}
