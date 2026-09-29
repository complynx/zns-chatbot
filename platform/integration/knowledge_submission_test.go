package integration_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func proposalSubmission(p knowledge.Proposal) knowledge.Submission {
	return knowledge.Submission{
		ProposalID: p.ID,
		Version:    p.Version,
		Event:      p.Event,
		Topic:      p.Topic,
		FactKey:    p.FactKey,
		Text:       p.Text,
	}
}

func submitKnowledgeProposal(t *testing.T, s knowledge.Service, actor string, p knowledge.Proposal) knowledge.Result {
	t.Helper()
	result, err := s.SubmitProposal(t.Context(), actor, proposalSubmission(p))
	require.NoError(t, err)
	return result
}

func TestKnowledgeAuthorSubmissionPrivateBoundary(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	ctx := t.Context()
	generation := int64(0)
	_, err := s.ExecuteDerived(
		ctx,
		"alice",
		knowledge.Command{
			Name:    knowledge.MemoSet,
			Key:     "private-input",
			FactKey: "private-source-id",
			Text:    "DO NOT PUBLISH PRIVATE MEMO",
		},
		readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
	)
	require.NoError(t, err)
	memo, err := s.Memo(ctx, "alice", "private-source-id")
	require.NoError(t, err)
	draft, err := s.ExecuteDerived(
		ctx,
		"alice",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "self-contained",
			Event:   "kb-current",
			Topic:   "travel",
			FactKey: "selected",
			Text:    "Meet at the station at noon.",
		},
		readsource.Derivation{Generation: &generation, PrivateHistory: true, Authorities: memo.ReadAuthorities},
	)
	require.NoError(t, err)
	assessed, err := s.Assess(
		ctx,
		"alice",
		knowledge.Assessment{
			Key:        "assessment",
			ProposalID: draft.Proposal.ID,
			Version:    1,
			Worthwhile: true,
			Reason:     "PRIVATE CLASSIFIER REASON",
		},
	)
	require.NoError(t, err)
	require.Equal(t, knowledge.AwaitingSubmission, assessed.Proposal.State)
	queue, err := s.Proposals(ctx, "bob", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Empty(t, queue)
	_, err = s.Execute(
		ctx,
		"bob",
		knowledge.Command{
			Name:       knowledge.Review,
			Key:        "before-consent",
			Event:      "kb-current",
			ProposalID: draft.Proposal.ID,
			Version:    2,
			Decision:   "approve",
		},
	)
	require.Error(t, err)
	consent := proposalSubmission(*assessed.Proposal)
	_, err = s.SubmitProposal(ctx, "bob", consent)
	require.Error(t, err)
	changed := consent
	changed.Text = "different text"
	_, err = s.SubmitProposal(ctx, "alice", changed)
	require.Error(t, err)
	changed = consent
	changed.Event = "kb-other"
	_, err = s.SubmitProposal(ctx, "alice", changed)
	require.Error(t, err)
	submitted, err := s.SubmitProposal(ctx, "alice", consent)
	require.NoError(t, err)
	require.True(t, submitted.Proposal.Submitted)
	require.Equal(t, "pending_review", submitted.Proposal.State)
	replay, err := s.SubmitProposal(ctx, "alice", consent)
	require.NoError(t, err)
	require.Equal(t, submitted.Proposal.Version, replay.Proposal.Version)
	changed = consent
	changed.Version--
	_, err = s.SubmitProposal(ctx, "alice", changed)
	require.Error(t, err)
	queue, err = s.Proposals(ctx, "bob", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Len(t, queue, 1)
	encoded, err := json.Marshal(queue)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-source-id")
	require.NotContains(t, string(encoded), "DO NOT PUBLISH")
	require.NotContains(t, string(encoded), "PRIVATE CLASSIFIER")
	require.Equal(t, "Meet at the station at noon.", queue[0].Text)
	require.Len(t, queue[0].ReadAuthorities, 1)
	require.Nil(t, queue[0].ReadAuthorities[0].Causal)
	_, err = s.DB.Exec(
		ctx,
		`DELETE FROM core.knowledge_permissions WHERE scope='kb-current' AND actor='bob' AND permission='review'`,
	)
	require.NoError(t, err)
	_, err = s.Proposals(ctx, "bob", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.Error(t, err)
	_, err = s.DB.Exec(
		ctx,
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('kb-current','bob','review')`,
	)
	require.NoError(t, err)
	queue, err = s.Proposals(ctx, "bob", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Len(t, queue, 1)

	// A legitimate second proposal carries materialized origin evidence without
	// disclosing that evidence to its own reviewers.
	child, err := s.ExecuteDerived(
		ctx,
		"bob",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "child",
			Event:   "kb-current",
			Topic:   "travel",
			FactKey: "child",
			Text:    "The meeting is at noon.",
		},
		readsource.Derivation{Generation: &generation, Authorities: queue[0].ReadAuthorities},
	)
	require.NoError(t, err)
	child, err = s.Assess(
		ctx,
		"bob",
		knowledge.Assessment{
			Key:        "child-assess",
			ProposalID: child.Proposal.ID,
			Version:    child.Proposal.Version,
			Worthwhile: true,
		},
	)
	require.NoError(t, err)
	child = submitKnowledgeProposal(t, s, "bob", *child.Proposal)
	reviewer, err := s.Proposals(ctx, "kbadmin", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Len(t, reviewer, 2)
	published, err := s.Execute(ctx, "bob", knowledge.Command{
		Name: knowledge.Review, Key: "publish-consented", Event: consent.Event,
		ProposalID: consent.ProposalID, Version: submitted.Proposal.Version, Decision: "approve",
	})
	require.NoError(t, err)
	require.Equal(t, consent.Text, published.Fact.Text)
	facts, err := s.Retrieve(ctx, "kbadmin", knowledge.Query{Event: consent.Event})
	require.NoError(t, err)
	require.Len(t, facts, 1)
	require.Equal(t, consent.Text, facts[0].Text)
	// Source retirement cannot be undone by consent replay or a different reviewer.
	history := conversation.Service{DB: s.DB}
	err = history.AppendOriginal(ctx, "alice", "submission-private-history", "user", "private history")
	require.NoError(t, err)
	var event int64
	require.NoError(
		t,
		s.DB.QueryRow(ctx, `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='submission-private-history'`).
			Scan(&event),
	)
	require.NoError(t, history.DeleteContent(ctx, "alice", event))
	_, err = s.SubmitProposal(ctx, "alice", consent)
	require.Error(t, err)
	reviewer, err = s.Proposals(ctx, "kbadmin", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Empty(t, reviewer)
	_, err = s.Execute(
		ctx,
		"kbadmin",
		knowledge.Command{
			Name:       knowledge.Review,
			Key:        "retired-child",
			Event:      "kb-current",
			ProposalID: child.Proposal.ID,
			Version:    child.Proposal.Version,
			Decision:   "approve",
		},
	)
	require.Error(t, err)
}

func TestKnowledgeSubmissionHostAdmissionAndConcurrentReplay(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	draft, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "draft",
			Event:   "kb-current",
			Topic:   "travel",
			FactKey: "consented",
			Text:    "Only this finished text.",
		},
	)
	require.NoError(t, err)
	assessed, err := s.Assess(
		t.Context(),
		"alice",
		knowledge.Assessment{
			Key:        "assess",
			ProposalID: draft.Proposal.ID,
			Version:    draft.Proposal.Version,
			Worthwhile: true,
		},
	)
	require.NoError(t, err)
	input := proposalSubmission(*assessed.Proposal)
	body, err := json.Marshal(input)
	require.NoError(t, err)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.Handler(appservices.NewServices(s.DB, appservices.Options{}), signer, slog.New(slog.DiscardHandler))
	for _, host := range []string{"", signer.DerivedMutationToken("bob")} {
		req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/submit", strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+signer.Token("alice"))
		req.Header.Set("X-Zns-Derivation", host)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
	}
	var count int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_proposal_submissions`).Scan(&count),
	)
	require.Zero(t, count)
	const attempts = 4
	results := make(chan int, attempts)
	var workers sync.WaitGroup
	for range attempts {
		workers.Go(func() {
			req := httptest.NewRequest(http.MethodPost, "/internal/knowledge/submit", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+signer.Token("alice"))
			req.Header.Set("X-Zns-Derivation", signer.DerivedMutationToken("alice"))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			results <- response.Code
		})
	}
	workers.Wait()
	close(results)
	for status := range results {
		require.Equal(t, http.StatusOK, status)
	}
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_proposal_submissions`).Scan(&count),
	)
	require.Equal(t, 1, count)
	queue, err := s.Proposals(t.Context(), "bob", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Len(t, queue, 1)
	require.Equal(t, input.Version+1, queue[0].Version)
	approved, err := s.Execute(
		t.Context(),
		"bob",
		knowledge.Command{
			Name:       knowledge.Review,
			Key:        "approve",
			Event:      input.Event,
			ProposalID: input.ProposalID,
			Version:    queue[0].Version,
			Decision:   "approve",
		},
	)
	require.NoError(t, err)
	require.Equal(t, "approved", approved.Proposal.State)
	replay, err := s.SubmitProposal(t.Context(), "alice", input)
	require.NoError(t, err)
	require.Equal(t, approved.Proposal.Version, replay.Proposal.Version)
}
