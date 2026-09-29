package integration_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// Idempotency preserves the whole public result. JSON compares timestamps by
// their wire value instead of pgx's OS-dependent [time.Location] representation.
func assertKnowledgeReplay(t *testing.T, expected, actual knowledge.Result) {
	t.Helper()
	before, err := json.Marshal(expected)
	require.NoError(t, err)
	after, err := json.Marshal(actual)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after))
}

func knowledgeFixture(t *testing.T) knowledge.Service {
	t.Helper()
	db := database(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.users(id,telegram_id,name) VALUES('kbadmin',990001,'Knowledge curator');
 INSERT INTO core.pass_events(id,finishes_at) VALUES('kb-past',clock_timestamp()-interval '1 hour'),('kb-current',clock_timestamp()+interval '1 hour'),('kb-other',clock_timestamp()+interval '2 hours');
 INSERT INTO core.knowledge_scopes(scope,event_id) VALUES('kb-past','kb-past'),('kb-current','kb-current'),('kb-other','kb-other');
 INSERT INTO core.knowledge_permissions(scope,actor,permission) SELECT scope,'kbadmin',p FROM core.knowledge_scopes CROSS JOIN unnest(ARRAY['curate','review']) AS p;
 INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('kb-current','bob','review')`,
	)
	require.NoError(t, err)
	return knowledge.Service{DB: db}
}

func knowledgeFact(t *testing.T, s knowledge.Service, event, key, text string, version int64) {
	t.Helper()
	result, err := s.Execute(
		t.Context(),
		"kbadmin",
		knowledge.Command{
			Name:    knowledge.Curate,
			Key:     event + key + strconv.FormatInt(version, 10),
			Event:   event,
			Topic:   "travel",
			FactKey: key,
			Text:    text,
			Version: version,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, result.Fact)
}

func TestKnowledgePriorityAndDynamicEventEnd(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	knowledgeFact(t, s, "", "venue", "General travel advice", 0)
	knowledgeFact(t, s, "kb-past", "venue", "Historical venue", 0)
	values, err := s.Retrieve(t.Context(), "alice", knowledge.Query{Event: "kb-current", Topic: "travel"})
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, "kb-past", values[0].Event)
	assert.Equal(t, "past", values[0].Phase)
	assert.True(t, values[0].HistoricalFallback)
	assert.True(t, values[0].Untrusted)
	knowledgeFact(t, s, "kb-current", "venue", "New venue", 0)
	values, err = s.Retrieve(t.Context(), "alice", knowledge.Query{Event: "kb-current"})
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, "New venue", values[0].Text)
	assert.Equal(t, "active_or_upcoming", values[0].Phase)
	assert.False(t, values[0].HistoricalFallback)
	values, err = s.Retrieve(t.Context(), "alice", knowledge.Query{Event: "kb-current", Text: "Historical"})
	require.NoError(t, err)
	assert.Empty(t, values, "search must not bypass the current-event override")
	values, err = s.Retrieve(t.Context(), "alice", knowledge.Query{})
	require.NoError(t, err)
	require.Len(t, values, 1)
	assert.Equal(t, "general", values[0].Phase)
	_, err = s.DB.Exec(
		t.Context(),
		`UPDATE core.pass_events SET finishes_at=clock_timestamp()+interval '200 milliseconds' WHERE id='kb-current'`,
	)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		facts, readErr := s.Retrieve(t.Context(), "alice", knowledge.Query{Event: "kb-current"})
		return readErr == nil && len(facts) == 1 && facts[0].Phase == "past"
	}, 2*time.Second, 20*time.Millisecond)
	var copied int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_facts WHERE scope='kb-current'`).Scan(&copied),
	)
	assert.Equal(t, 1, copied, "event transition classifies the same row without a copying job")
}

func TestKnowledgeSuggestionFilterReviewRightsAndReplay(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	command := knowledge.Command{
		Name:    knowledge.Suggest,
		Key:     "suggest",
		Event:   "kb-current",
		Topic:   "travel",
		FactKey: "venue",
		Text:    "Suggested venue",
	}
	first, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.NotNil(t, first.Proposal)
	assert.Equal(t, "pending_filter", first.Proposal.State)
	repeated, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assertKnowledgeReplay(t, first, repeated)
	command.Text = "different"
	_, err = s.Execute(t.Context(), "alice", command)
	requireCode(t, err, "idempotency_conflict")
	facts, err := s.Retrieve(t.Context(), "bob", knowledge.Query{Event: "kb-current"})
	require.NoError(t, err)
	assert.Empty(t, facts)
	other, err := s.Proposals(t.Context(), "bob", knowledge.ProposalQuery{Event: "kb-current"})
	require.NoError(t, err)
	assert.Empty(t, other)
	review := knowledge.Command{
		Name:       knowledge.Review,
		Key:        "review",
		Event:      "kb-current",
		ProposalID: first.Proposal.ID,
		Version:    1,
		Decision:   "approve",
	}
	_, err = s.Execute(t.Context(), "bob", review)
	requireCode(t, err, "knowledge_review_state")
	assessment := knowledge.Assessment{
		Key:        "filter",
		ProposalID: first.Proposal.ID,
		Version:    1,
		Worthwhile: true,
		Reason:     "Specific event fact",
	}
	_, err = s.Assess(t.Context(), "bob", assessment)
	requireCode(t, err, "knowledge_not_found")
	filtered, err := s.Assess(t.Context(), "alice", assessment)
	require.NoError(t, err)
	assert.Equal(t, knowledge.AwaitingSubmission, filtered.Proposal.State)
	filtered = submitKnowledgeProposal(t, s, "alice", *filtered.Proposal)
	queue, err := s.Proposals(t.Context(), "bob", knowledge.ProposalQuery{Event: "kb-current", ReviewQueue: true})
	require.NoError(t, err)
	require.Len(t, queue, 1)
	_, err = s.Proposals(t.Context(), "bob", knowledge.ProposalQuery{Event: "kb-other", ReviewQueue: true})
	requireCode(t, err, "forbidden")
	_, err = s.Execute(t.Context(), "bob", review)
	requireCode(t, err, "knowledge_stale")
	review.Version = filtered.Proposal.Version
	_, err = s.DB.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('kb-current','alice','review')`,
	)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", review)
	requireCode(t, err, "forbidden")
	review.Event = "kb-other"
	_, err = s.Execute(t.Context(), "bob", review)
	requireCode(t, err, "forbidden")
	review.Event = "kb-current"
	approved, err := s.Execute(t.Context(), "bob", review)
	require.NoError(t, err)
	assert.Equal(t, "approved", approved.Proposal.State)
	require.NotNil(t, approved.Fact)
	again, err := s.Execute(t.Context(), "bob", review)
	require.NoError(t, err)
	assertKnowledgeReplay(t, approved, again)
	var audits int
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.knowledge_audit WHERE scope='kb-current'`).Scan(&audits),
	)
	assert.Equal(t, 3, audits)
	_, err = s.DB.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='bob'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "bob", review)
	requireCode(t, err, "forbidden")
}

func TestKnowledgeRejectAndConcurrentApproval(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	for _, worthwhile := range []bool{false, true} {
		key := strconv.FormatBool(worthwhile)
		proposal, err := s.Execute(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.Suggest,
				Key:     key,
				Event:   "kb-current",
				Topic:   "rules",
				FactKey: key,
				Text:    "A specific suggestion",
			},
		)
		require.NoError(t, err)
		filtered, err := s.Assess(
			t.Context(),
			"alice",
			knowledge.Assessment{
				Key:        "filter-" + key,
				ProposalID: proposal.Proposal.ID,
				Version:    1,
				Worthwhile: worthwhile,
			},
		)
		require.NoError(t, err)
		if !worthwhile {
			assert.Equal(t, "filtered", filtered.Proposal.State)
			continue
		}
		filtered = submitKnowledgeProposal(t, s, "alice", *filtered.Proposal)
		command := knowledge.Command{
			Name:       knowledge.Review,
			Key:        "review",
			Event:      "kb-current",
			ProposalID: proposal.Proposal.ID,
			Version:    filtered.Proposal.Version,
			Decision:   "approve",
		}
		var wg sync.WaitGroup
		outcomes := make(chan error, 2)
		for _, actor := range []string{"bob", "kbadmin"} {
			wg.Go(func() { _, executeErr := s.Execute(t.Context(), actor, command); outcomes <- executeErr })
		}
		wg.Wait()
		close(outcomes)
		successes := 0
		for outcome := range outcomes {
			if outcome == nil {
				successes++
			} else {
				requireCode(t, outcome, "knowledge_stale")
			}
		}
		assert.Equal(t, 1, successes)
	}
}

func TestKnowledgeMemoPrivacyCapacityAndVersions(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	for index := range knowledge.MaxMemos {
		_, err := s.Execute(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.MemoSet,
				Key:     strconv.Itoa(index),
				FactKey: strconv.Itoa(index),
				Text:    "private preference",
			},
		)
		require.NoError(t, err)
	}
	_, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "overflow", FactKey: "extra", Text: "extra"},
	)
	requireCode(t, err, "knowledge_memo_capacity")
	for _, actor := range []string{"bob", "kbadmin"} {
		memos, readErr := s.Memos(t.Context(), actor)
		require.NoError(t, readErr)
		assert.Empty(t, memos)
		memo, readErr := s.Memo(t.Context(), actor, "0")
		require.NoError(t, readErr)
		assert.Zero(t, memo.Version)
	}
	command := knowledge.Command{
		Name:    knowledge.MemoSet,
		Key:     "edit",
		FactKey: "0",
		Text:    "new private preference",
		Version: 1,
	}
	updated, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.EqualValues(t, 2, updated.Memo.Version)
	command.Key = "stale"
	_, err = s.Execute(t.Context(), "alice", command)
	requireCode(t, err, "knowledge_stale")
	deleted, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoDelete, Key: "delete", FactKey: "0", Version: 2},
	)
	require.NoError(t, err)
	assert.Empty(t, deleted.Memo.Text)
	assert.False(t, deleted.Memo.Active)
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "space", FactKey: "extra", Text: "new item"},
	)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.Curate, Key: "no-role", Topic: "rules", FactKey: "x", Text: "not authorized"},
	)
	requireCode(t, err, "forbidden")
}

func TestKnowledgeAPIRejectsActorAndFilterInjection(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.Handler(appservices.NewServices(s.DB, appservices.Options{}), signer, slog.New(slog.DiscardHandler))
	for _, test := range []struct {
		body, token string
		status      int
	}{
		{`{"name":"memo_set","key":"set","fact_key":"note","text":"private","owner":"bob"}`, signer.Token("alice"), 400},
		{`{"name":"assess","key":"set","proposal_id":1,"decision":"approve"}`, signer.Token("alice"), 403},
		{`{"name":"memo_set","key":"set","fact_key":"note","text":"private"}`, "", 401},
		{`{"name":"memo_set","key":"set","fact_key":"note","text":"private"}`, signer.Token("alice"), 200},
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/knowledge/actions", strings.NewReader(test.body))
		request.Header.Set("Authorization", "Bearer "+test.token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/me/memos?owner=alice", nil)
	request.Header.Set("Authorization", "Bearer "+signer.Token("bob"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assert.Equal(t, 200, recorder.Code)
	var memos knowledge.MemoPage
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &memos))
	assert.Empty(t, memos.Items)
}

func TestKnowledgeReviewCannotOverwriteNewerFact(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	knowledgeFact(t, s, "kb-current", "venue", "Initial venue", 0)
	proposal, err := s.Execute(t.Context(), "alice", knowledge.Command{
		Name:    knowledge.Suggest,
		Key:     "suggest",
		Event:   "kb-current",
		Topic:   "travel",
		FactKey: "venue",
		Text:    "Suggested replacement",
	})
	require.NoError(t, err)
	assessed, err := s.Assess(
		t.Context(),
		"alice",
		knowledge.Assessment{Key: "filter", ProposalID: proposal.Proposal.ID, Version: 1, Worthwhile: true},
	)
	require.NoError(t, err)
	assessed = submitKnowledgeProposal(t, s, "alice", *assessed.Proposal)
	knowledgeFact(t, s, "kb-current", "venue", "Curator correction", 1)
	review := knowledge.Command{
		Name:       knowledge.Review,
		Key:        "approve",
		Event:      "kb-current",
		ProposalID: proposal.Proposal.ID,
		Version:    assessed.Proposal.Version,
		Decision:   "approve",
	}
	_, err = s.Execute(t.Context(), "bob", review)
	requireCode(t, err, "knowledge_stale")
	review.Key, review.Decision = "reject", "reject"
	rejected, err := s.Execute(t.Context(), "bob", review)
	require.NoError(t, err)
	assert.Equal(t, "rejected", rejected.Proposal.State)
	facts, err := s.Retrieve(t.Context(), "alice", knowledge.Query{Event: "kb-current"})
	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.Equal(t, "Curator correction", facts[0].Text)
	_, err = s.Execute(
		t.Context(),
		"kbadmin",
		knowledge.Command{
			Name:    knowledge.RemoveFact,
			Key:     "remove",
			Event:   "kb-current",
			Topic:   "travel",
			FactKey: "venue",
			Version: 2,
		},
	)
	require.NoError(t, err)
	facts, err = s.Retrieve(t.Context(), "alice", knowledge.Query{Event: "kb-current"})
	require.NoError(t, err)
	assert.Empty(t, facts)
}

func TestKnowledgeSearchAndTextBounds(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	knowledgeFact(t, s, "", "literal", "Discount is 10%_off", 0)
	knowledgeFact(t, s, "", "plain", "Another fact", 0)
	facts, err := s.Retrieve(t.Context(), "alice", knowledge.Query{Text: "%_"})
	require.NoError(t, err)
	require.Len(t, facts, 1)
	assert.Equal(t, "literal", facts[0].Key)
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.Suggest,
			Key:     "large",
			Topic:   "rules",
			FactKey: "large",
			Text:    strings.Repeat("a", knowledge.MaxText+1),
		},
	)
	requireCode(t, err, "knowledge_invalid")
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.MemoSet,
			Key:     "large-memo",
			FactKey: "large",
			Text:    strings.Repeat("a", knowledge.MaxMemoText+1),
		},
	)
	requireCode(t, err, "knowledge_invalid")
	for index := range knowledge.MaxPending {
		_, err = s.Execute(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.Suggest,
				Key:     strconv.Itoa(index),
				Topic:   "rules",
				FactKey: strconv.Itoa(index),
				Text:    "bounded pending suggestion",
			},
		)
		require.NoError(t, err)
	}
	_, err = s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.Suggest, Key: "overflow", Topic: "rules", FactKey: "overflow", Text: "extra"},
	)
	requireCode(t, err, "knowledge_pending_capacity")
}
