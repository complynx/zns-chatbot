package bot

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const (
	knowledgeReplayReceiptPath    = "/internal/knowledge/derived/receipt"
	knowledgeReplayAttachmentPath = "/internal/memory/sources"
)

type knowledgeReplayCalls struct {
	posts map[string]int
	mu    sync.Mutex
}

func (c *knowledgeReplayCalls) snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return maps.Clone(c.posts)
}

// The server answers only the authorized receipt and preferences. Attachment,
// command effects and assessment are all POSTs that fail, so any fallback into
// them is observable both as a counted call and as a returned error.
func knowledgeReplayBot(t *testing.T, db *pgxpool.Pool, result knowledge.Result) (*Bot, *knowledgeReplayCalls) {
	t.Helper()
	calls := &knowledgeReplayCalls{posts: map[string]int{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			calls.mu.Lock()
			calls.posts[r.URL.Path]++
			calls.mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case knowledgeReplayReceiptPath:
			_ = json.NewEncoder(w).Encode(derivedmutation.Receipt[knowledge.Result]{Found: true, Result: result})
		case "/v1/me/preferences":
			_ = json.NewEncoder(w).Encode(account.Preferences{Language: "en"})
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(server.Close)
	b := &Bot{DB: db, API: appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token}}
	b.Host = appclient.Host{Base: server.URL, UserToken: b.API.UserToken}
	return b, calls
}

func recordKnowledgeReplyForTest(t *testing.T, db *pgxpool.Pool, owner string, id int64, text string) {
	t.Helper()
	raw, err := json.Marshal(text)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES($1,$2,'knowledge_reply',$3)`, owner, id, raw)
	require.NoError(t, err)
}

// A completed suggestion receipt still carries its original pending_filter
// state. Replay must be handled from the durable reply (including a reply
// recorded after a deferred assessment) without attachment, command effects,
// assessment or the pending continuation; only an unfinished turn continues.
func TestCompletedSuggestReceiptReplaysRecordedReply(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	translate := func(id i18n.ID) string {
		text, err := i18n.Translate("en", id, nil)
		require.NoError(t, err)
		return text
	}
	pendingFilter, suggested := translate(i18n.KnowledgePendingFilter), translate(i18n.KnowledgeSuggested)
	unavailable := translate(i18n.KnowledgeUnavailable)
	require.NotEqual(t, pendingFilter, suggested)
	command := knowledge.Command{
		Name:    knowledge.Suggest,
		Event:   "synthetic-replay-event",
		Topic:   "travel",
		FactKey: "replay",
		Text:    "Synthetic suggestion body",
	}
	plan := interaction.SavedPlan{KnowledgeCommand: &command}
	pending := knowledge.Result{Proposal: &knowledge.Proposal{
		ID: 7, State: knowledgePendingFilter, Topic: command.Topic, FactKey: command.FactKey, Text: command.Text,
	}}
	for _, scenario := range []struct {
		result   knowledge.Result
		name     string
		recorded string
		want     string
		id       int64
	}{
		{pending, "deferred_assessment", pendingFilter, pendingFilter, 10701},
		{pending, "assessed", suggested, suggested, 10702},
		{knowledge.Result{Redacted: true}, "redacted", pendingFilter, unavailable, 10703},
	} {
		recordKnowledgeReplyForTest(t, db, "bob", scenario.id, scenario.recorded)
		b, calls := knowledgeReplayBot(t, db, scenario.result)
		text, found, err := b.savedKnowledgeReceipt(t.Context(), incoming{owner: "bob"}, scenario.id, plan, r36Source())
		require.NoError(t, err, scenario.name)
		require.True(t, found, "%s: completed replay is handled and never falls through to execution", scenario.name)
		require.Equal(t, scenario.want, text, scenario.name)
		require.NotContains(t, text, command.Text, scenario.name)
		require.Equal(t, map[string]int{knowledgeReplayReceiptPath: 1}, calls.snapshot(),
			"%s: replay performs no attachment, command effect or assessment", scenario.name)
		view, err := b.currentKnowledgeView(t.Context(), "bob")
		require.NoError(t, err)
		require.Equal(t, knowledgeView{Event: command.Event, Mode: knowledgeOwnMode}, view, scenario.name)
	}

	// Another owner's reply for the same update does not complete bob's turn:
	// the unfinished retry still attaches original sources and stays retryable.
	const unfinished int64 = 10704
	recordKnowledgeReplyForTest(t, db, "alice", unfinished, suggested)
	b, calls := knowledgeReplayBot(t, db, pending)
	text, _, err := b.savedKnowledgeReceipt(t.Context(), incoming{owner: "bob"}, unfinished, plan, r36Source())
	require.Error(t, err, "an interrupted attachment is returned for retry")
	require.Empty(t, text)
	posts := calls.snapshot()
	require.Equal(t, 1, posts[knowledgeReplayReceiptPath])
	require.NotZero(t, posts[knowledgeReplayAttachmentPath], "unfinished recovery still attaches original sources")
	_, recorded, err := b.recordedKnowledgeReply(t.Context(), "bob", unfinished)
	require.NoError(t, err)
	require.False(t, recorded, "a failed attachment records no terminal reply")
}

// The completed-replay boundary is the durable knowledge reply of this owner
// and update only; other kinds, updates and owners leave recovery unfinished.
func TestKnowledgeReplyRecordedIsOwnerUpdateScoped(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	b := Bot{DB: db}
	const update int64 = 88931
	_, recorded, err := b.recordedKnowledgeReply(t.Context(), "bob", update)
	require.NoError(t, err)
	require.False(t, recorded)
	for _, row := range []struct {
		owner string
		id    int64
		kind  string
	}{
		{"alice", update, "knowledge_reply"},
		{"bob", update + 1, "knowledge_reply"},
		{"bob", update, "reply"},
		{"bob", update, "input"},
	} {
		_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES($1,$2,$3,'"synthetic"')`, row.owner, row.id, row.kind)
		require.NoError(t, err)
	}
	_, recorded, err = b.recordedKnowledgeReply(t.Context(), "bob", update)
	require.NoError(t, err)
	require.False(t, recorded, "an unfinished attachment has no knowledge reply for this update")
	_, err = db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES('bob',$1,'knowledge_reply','"saved"')`, update)
	require.NoError(t, err)
	reply, recorded, err := b.recordedKnowledgeReply(t.Context(), "bob", update)
	require.NoError(t, err)
	require.True(t, recorded, "a recorded knowledge reply marks completed replay")
	require.Equal(t, "saved", reply)

	db.Close()
	_, recorded, err = b.recordedKnowledgeReply(t.Context(), "bob", update)
	require.Error(t, err, "a failed completion read is returned, never treated as unfinished")
	require.False(t, recorded)
}
