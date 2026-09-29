package integration_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestMemoryHierarchySearchAndPrivateIsolation(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	knowledgeFact(t, s, "kb-past", "venue", "Old needle venue", 0)
	knowledgeFact(t, s, "kb-current", "venue", "Current hall", 0)
	knowledgeFact(t, s, "kb-past", "bus", "Blue shuttle", 0)
	_, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.MemoSet,
			Key:     "private",
			FactKey: "travel.note",
			Text:    "Only Alice needle",
			Version: 0,
		},
	)
	require.NoError(t, err)
	page, err := s.SearchMemory(t.Context(), "bob", knowledge.MemoryQuery{Event: "kb-current", Text: "needle"})
	require.NoError(t, err)
	assert.Empty(t, page.Entries)
	page, err = s.SearchMemory(t.Context(), "alice", knowledge.MemoryQuery{Event: "kb-current", Text: "needle"})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	assert.Equal(t, knowledge.MemoryPrivate, page.Entries[0].Namespace)
	privateRef := page.Entries[0].Ref
	history, err := s.MemoryHistory(t.Context(), "bob", privateRef, "")
	require.NoError(t, err)
	assert.Empty(t, history.Entries)
	for _, mode := range []string{knowledge.MemoryLiteral, knowledge.MemoryRegex, knowledge.MemoryText} {
		page, err = s.SearchMemory(
			t.Context(),
			"alice",
			knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Event: "kb-current", Text: "Blue", Mode: mode},
		)
		require.NoError(t, err)
		require.Len(t, page.Entries, 1)
		assert.True(t, page.Entries[0].HistoricalFallback)
		detail, readErr := s.ReadMemory(t.Context(), "alice", page.Entries[0].Ref)
		require.NoError(t, readErr)
		assert.Equal(t, "Blue shuttle", detail.Text)
		assert.True(t, detail.HistoricalFallback)
		assert.Equal(t, "past", detail.Phase)
	}
	_, err = s.SearchMemory(t.Context(), "alice", knowledge.MemoryQuery{Mode: knowledge.MemoryRegex, Text: "("})
	require.Error(t, err)
}

func TestMemoryHierarchyFreshnessHistoryAndSummary(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	text := strings.Repeat("Long detail ", 30)
	command := knowledge.Command{Name: knowledge.MemoSet, Key: "summary1", FactKey: "summary.overview", Text: text}
	_, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	summary, err := s.MemorySummary(t.Context(), "alice", knowledge.MemoryQuery{})
	require.NoError(t, err)
	require.Len(t, summary.Summaries, 1)
	assert.Less(t, len(summary.Summaries[0].Text), len(text))
	ref := summary.Summaries[0].Ref
	detail, err := s.ReadMemory(t.Context(), "alice", ref)
	require.NoError(t, err)
	assert.Equal(t, text, detail.Text)
	command.Key, command.Version, command.Text = "summary2", 1, "Updated summary"
	result, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	replay, err := s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assertKnowledgeReplay(t, result, replay)
	_, err = s.ReadMemory(t.Context(), "alice", ref)
	requireCode(t, err, "knowledge_stale")
	history, err := s.MemoryHistory(t.Context(), "alice", ref, "")
	require.NoError(t, err)
	require.Len(t, history.Entries, 2)
	assert.True(t, history.Entries[0].Historical)
	assert.NotNil(t, history.Entries[0].CapturedAt)
	command.Name, command.Key, command.Version, command.Text = knowledge.MemoDelete, "summary3", 2, ""
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	summary, err = s.MemorySummary(t.Context(), "alice", knowledge.MemoryQuery{})
	require.NoError(t, err)
	assert.Empty(t, summary.Summaries)
	assert.Empty(t, summary.Topics)
	history, err = s.MemoryHistory(t.Context(), "alice", ref, "")
	require.NoError(t, err)
	require.Len(t, history.Entries, 3)
	for _, entry := range history.Entries {
		assert.Empty(t, entry.Text)
	}
}

func TestMemoryHierarchyBoundedScanAndCursorBinding(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	const total = 105
	for i := range total {
		knowledgeFact(t, s, "", fmt.Sprintf("item_%03d", i), "Unmatched content", 0)
	}
	q := knowledge.MemoryQuery{Namespace: knowledge.MemoryShared, Text: "needle", Mode: knowledge.MemoryRegex}
	page, err := s.SearchMemory(t.Context(), "alice", q)
	require.NoError(t, err)
	assert.Empty(t, page.Entries)
	assert.True(t, page.More)
	assert.True(t, page.Incomplete)
	require.NotEmpty(t, page.NextCursor)
	q.Cursor = page.NextCursor
	last, err := s.SearchMemory(t.Context(), "alice", q)
	require.NoError(t, err)
	assert.False(t, last.More)
	assert.Equal(t, total, page.Scanned+last.Scanned)
	_, err = s.SearchMemory(t.Context(), "bob", q)
	require.Error(t, err)
	q.Text = "other"
	_, err = s.SearchMemory(t.Context(), "alice", q)
	require.Error(t, err)
	q = knowledge.MemoryQuery{Namespace: knowledge.MemoryShared}
	seen := map[string]bool{}
	for {
		page, err = s.SearchMemory(t.Context(), "alice", q)
		require.NoError(t, err)
		for _, entry := range page.Entries {
			assert.False(t, seen[entry.Ref])
			seen[entry.Ref] = true
		}
		if !page.More {
			break
		}
		q.Cursor = page.NextCursor
	}
	assert.Len(t, seen, total)
}

func TestMemoryHierarchyAPIAuthenticationAndReferences(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	_, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{
			Name:    knowledge.MemoSet,
			Key:     "api-note",
			FactKey: "summary.overview",
			Text:    "Owner-only summary",
		},
	)
	require.NoError(t, err)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.Handler(runtimeapp.NewServices(s.DB, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler))
	request := func(actor, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if actor != "" {
			req.Header.Set("Authorization", "Bearer "+signer.Token(actor))
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder
	}
	for _, path := range []string{"summary", "index", "search", "read", "history"} {
		assert.Equal(t, http.StatusUnauthorized, request("", "/v1/memory/"+path).Code)
	}
	response := request("alice", "/v1/memory/index?namespace=private")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var page knowledge.MemoryPage
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Entries, 1)
	ref := url.QueryEscape(page.Entries[0].Ref)
	response = request("alice", "/v1/memory/read?ref="+ref)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var detail knowledge.MemoryEntry
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &detail))
	assert.Equal(t, "Owner-only summary", detail.Text)
	for _, path := range []string{"summary?owner=alice", "index?owner=alice", "history?ref=" + ref + "&owner=alice"} {
		response = request("bob", "/v1/memory/"+path)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.NotContains(t, response.Body.String(), "Owner-only summary")
	}
	response = request("alice", "/v1/memory/search?mode=regex&q=%28")
	assert.Equal(t, http.StatusBadRequest, response.Code)
}
