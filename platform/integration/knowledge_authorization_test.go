package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestKnowledgeAuthorizationAtEveryModelBoundary(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"knowledge", "history", "retry", "database_failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := knowledgeAuthorizationFixture(t)
			calls := 0
			f.b.Model = knowledgeBoundaryModel(t, f, scenario, &calls)
			update := message(998, identity.BobTelegramID, "Read pending proposals and supporting context")
			if scenario == "retry" {
				require.ErrorIs(t, f.b.Handle(t.Context(), update), context.Canceled)
			}
			if scenario == "database_failure" {
				assertKnowledgeOutage(t, f, update, f.b.Handle(t.Context(), update))
				require.Equal(t, 2, calls)
				recoverKnowledgeOutage(t, f, update, "unavailable_knowledge_permissions")
				require.Equal(t, 2, calls, "restart uses a fresh model instance")
				return
			}
			handle(t, f.b, update)
			expected := map[string]int{"knowledge": 3, "history": 3, "retry": 4, "database_failure": 2}
			assert.Equal(t, expected[scenario], calls)
		})
	}
}

func TestProviderRefreshesKnowledgeAndRegistrationAfterSelection(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"revoke", "knowledge_only", "registration_only", "database_failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			f := registrationPaymentFixture(t)
			seedProviderPrivateReads(t, f)
			var calls atomic.Int32
			observations := make(chan providerSelectionObservation, 8)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				first := calls.Add(1) == 1
				body, requestErr := io.ReadAll(r.Body)
				if requestErr == nil {
					requestErr = writeProviderSelectionResponse(w, r, f, scenario, first)
				}
				observations <- providerSelectionObservation{body: string(body), err: requestErr}
			}))
			defer server.Close()
			f.b.Model = agent.OpenAI{Key: "synthetic", HTTP: server.Client(), BaseURL: server.URL}
			update := message(997, identity.BobTelegramID, "Review the available context")
			err := f.b.Handle(t.Context(), update)
			assertProviderSelectionInputs(t, scenario, observations, calls.Load())
			if scenario == "database_failure" {
				assertKnowledgeOutage(t, f, update, err)
				require.Equal(t, int32(1), calls.Load())
				recoverKnowledgeOutage(t, f, update, "unavailable_provider_permissions")
			} else {
				assertProviderRevocation(t, f, update, err)
			}
			expected := int32(2)
			if scenario == "database_failure" {
				expected = 1
			}
			assert.Equal(t, expected, calls.Load())
		})
	}
}

type providerSelectionObservation struct {
	body string
	err  error
}

func writeProviderSelectionResponse(
	w http.ResponseWriter,
	r *http.Request,
	f *fixture,
	scenario string,
	first bool,
) error {
	text := `{"text":"Authorized answer","view":"workflow","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null}`
	if first {
		if _, err := f.db.Exec(r.Context(), providerSelectionMutation(scenario)); err != nil {
			http.Error(w, "fixture mutation failed", http.StatusInternalServerError)
			return err
		}
		text = `{"skills":["knowledge","registration"],"reply_language":"en"}`
	}
	encoded, err := json.Marshal(text)
	if err != nil {
		return err
	}
	_, err = w.Write(
		[]byte(
			`{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":` + string(
				encoded,
			) + `}]}]}`,
		),
	)
	return err
}

func providerSelectionMutation(scenario string) string {
	switch scenario {
	case "knowledge_only":
		return `DELETE FROM core.knowledge_permissions WHERE actor='bob'`
	case "registration_only":
		return `DELETE FROM core.pass_booking_admins WHERE owner='bob'`
	case "database_failure":
		return `ALTER TABLE core.knowledge_permissions RENAME TO unavailable_provider_permissions`
	default:
		return `DELETE FROM core.knowledge_permissions WHERE actor='bob'; DELETE FROM core.pass_booking_admins WHERE owner='bob'`
	}
}

func assertProviderSelectionInputs(
	t *testing.T,
	scenario string,
	observations <-chan providerSelectionObservation,
	calls int32,
) {
	t.Helper()
	for index := range calls {
		observation := <-observations
		require.NoError(t, observation.err)
		if index == 0 {
			assert.Contains(t, observation.body, "Private pending proposal")
			assert.Contains(t, observation.body, "PRIVATE REGISTRATION MARKER")
			continue
		}
		if scenario == "registration_only" {
			assert.Contains(t, observation.body, "Private pending proposal")
		} else {
			assert.NotContains(t, observation.body, "Private pending proposal")
		}
		if scenario == "knowledge_only" {
			assert.Contains(t, observation.body, "PRIVATE REGISTRATION MARKER")
		} else {
			assert.NotContains(t, observation.body, "PRIVATE REGISTRATION MARKER")
		}
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
INSERT INTO core.knowledge_proposals(scope,owner,topic,fact_key,body,fact_version,state) VALUES('review-event-1','alice','travel','private-review','Private pending proposal',0,'awaiting_submission')`)
	require.NoError(t, err)
	submitAuthorizationProposal(t, f, "review-event-1")
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
VALUES('','alice','travel','private-review','Private pending proposal',0,'awaiting_submission')`)
	require.NoError(t, err)
	submitAuthorizationProposal(t, f, "")
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

func assertKnowledgeOutage(t *testing.T, f *fixture, update telegram.Update, err error) {
	t.Helper()
	require.ErrorContains(t, err, "registration authority unavailable")
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, http.StatusInternalServerError, problem.Status)
	require.Equal(t, "internal_error", problem.Code)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM interaction.saved_turns WHERE owner='bob' AND update_id=$1`, update.ID).
			Scan(&count),
	)
	require.Zero(t, count, "outage creates neither a winner, paid notice nor terminal")
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind IN ('reply','reply_origin')`, update.ID).
			Scan(&count),
	)
	require.Zero(t, count, "outage must not persist an answer or paid-failure notice")
	assertKnowledgeAnswerAbsent(t, f, update.ID)
}

func assertKnowledgeAnswerAbsent(t *testing.T, f *fixture, updateID int64) {
	t.Helper()
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE owner='bob' AND source_key=$1`, "tg-assistant-"+strconv.FormatInt(updateID, 10)).
			Scan(&count),
	)
	require.Zero(t, count, "no derived answer is archived")
	cards, err := json.Marshal(chatMessages(t, f, identity.BobTelegramID))
	require.NoError(t, err)
	require.NotContains(t, string(cards), "Authorized answer")
	require.NotContains(t, string(cards), "The allowed read completed.")
	require.NotContains(t, string(cards), "Private pending proposal")
	require.NotContains(t, string(cards), "PRIVATE REGISTRATION MARKER")
}

func knowledgeQuotaReservations(t *testing.T, f *fixture) []string {
	t.Helper()
	rows, err := f.db.Query(
		t.Context(),
		`SELECT row_to_json(q)::text FROM bot.agent_quota q WHERE owner='bob' ORDER BY update_id`,
	)
	require.NoError(t, err)
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		require.NoError(t, rows.Scan(&key))
		keys = append(keys, key)
	}
	require.NoError(t, rows.Err())
	return keys
}

func recoverKnowledgeOutage(t *testing.T, f *fixture, update telegram.Update, table string) {
	t.Helper()
	keys := knowledgeQuotaReservations(t, f)
	require.Len(t, keys, 1)
	// Only the two fixed fixture table names are accepted for restoration.
	require.Contains(t, []string{"unavailable_knowledge_permissions", "unavailable_provider_permissions"}, table)
	_, err := f.db.Exec(t.Context(), `ALTER TABLE core.`+table+` RENAME TO knowledge_permissions`)
	require.NoError(t, err)
	calls := 0
	f.b = &bot.Bot{
		DB:  f.db,
		API: f.b.API, Host: f.b.Host,
		TG: f.b.TG,
		Model: avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
			calls++
			require.NotEmpty(t, input.Knowledge.Reads)
			require.NotEmpty(
				t,
				input.Knowledge.Reads[0].Proposals,
				"restored authority preserves authorized private context",
			)
			return agent.Plan{View: "workflow", Text: "Recovered authorized answer"}, nil
		}),
	}
	handle(t, f.b, update)
	require.Equal(t, 1, calls)
	handle(t, f.b, update)
	require.Equal(t, 1, calls, "saved winner retry never repeats model work")
	require.Equal(t, keys, knowledgeQuotaReservations(t, f))
	plan, err := (interaction.Store{DB: f.db}).Load(t.Context(), "bob", update.ID)
	require.NoError(t, err)
	require.Equal(t, interaction.Ready, plan.State)
	require.Empty(t, plan.SystemNotice)
	require.Equal(t, "Recovered authorized answer", plan.Plan.Text)
	cards, err := json.Marshal(chatMessages(t, f, identity.BobTelegramID))
	require.NoError(t, err)
	require.Contains(t, string(cards), "Recovered authorized answer")
}

func assertProviderRevocation(t *testing.T, f *fixture, update telegram.Update, firstErr error) {
	t.Helper()
	require.ErrorContains(t, firstErr, "terminal registration plan")
	saved, err := (interaction.Store{DB: f.db}).Load(t.Context(), "bob", update.ID)
	require.NoError(t, err)
	require.Equal(t, interaction.PrivacyTerminal, saved.State)
	require.Equal(t, interaction.SourceRevoked, saved.TerminalReason)
	require.Empty(t, saved.Plan.Text)
	assertKnowledgeAnswerAbsent(t, f, update.ID)
	keys := knowledgeQuotaReservations(t, f)
	require.Len(t, keys, 1)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','bob','review') ON CONFLICT DO NOTHING; INSERT INTO core.pass_booking_admins(owner) VALUES('bob') ON CONFLICT DO NOTHING`,
	)
	require.NoError(t, err)
	calls := 0
	f.b = &bot.Bot{
		DB:  f.db,
		API: f.b.API, Host: f.b.Host,
		TG: f.b.TG,
		Model: avModel(func(_ context.Context, _ agent.Input) (agent.Plan, error) {
			calls++
			return agent.Plan{View: "workflow", Text: "Fresh authorized request"}, nil
		}),
	}
	for range 2 {
		require.ErrorContains(t, f.b.Handle(t.Context(), update), "terminal registration plan")
	}
	require.Zero(t, calls, "restored grants cannot revive the old turn")
	require.NoError(t, f.b.Render(t.Context(), "bob", identity.BobTelegramID))
	assertKnowledgeAnswerAbsent(t, f, update.ID)
	require.Equal(t, keys, knowledgeQuotaReservations(t, f))
	fresh := message(1997, identity.BobTelegramID, "Make a fresh authorized request")
	handle(t, f.b, fresh)
	require.Equal(t, 1, calls)
	require.Len(t, knowledgeQuotaReservations(t, f), 2)
	cards, err := json.Marshal(chatMessages(t, f, identity.BobTelegramID))
	require.NoError(t, err)
	require.Contains(t, string(cards), "Fresh authorized request")
	assertKnowledgeAnswerAbsent(t, f, update.ID)
}

func knowledgeBoundaryModel(t *testing.T, f *fixture, scenario string, calls *int) agent.Model {
	t.Helper()
	return avModel(func(ctx context.Context, input agent.Input) (agent.Plan, error) {
		(*calls)++
		switch *calls {
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
			if scenario == "retry" && *calls == 3 {
				return knowledgeAuthorizationNextRead("knowledge"), nil
			}
			return agent.Plan{View: "workflow", Text: "The allowed read completed."}, nil
		}
	})
}

func submitAuthorizationProposal(t *testing.T, f *fixture, event string) {
	t.Helper()
	service := knowledge.Service{DB: f.db}
	proposals, err := service.Proposals(t.Context(), "alice", knowledge.ProposalQuery{Event: event})
	require.NoError(t, err)
	require.Len(t, proposals, 1)
	submitKnowledgeProposal(t, service, "alice", proposals[0])
}
