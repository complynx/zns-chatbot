package integration_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestMemoryServiceAudienceActorAndSourceBinding(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.Handler(appservices.NewServices(s.DB, appservices.Options{}), signer, slog.New(slog.DiscardHandler))
	_, err := s.Execute(
		t.Context(),
		"alice",
		knowledge.Command{Name: knowledge.MemoSet, Key: "owned-write", FactKey: "note", Text: "Owned memo"},
	)
	require.NoError(t, err)
	archive := conversation.Service{DB: s.DB}
	require.NoError(t, archive.AppendOriginal(t.Context(), "alice", "tg-user-55", "user", "Alice source"))
	call := func(token, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/internal/memory/sources", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	const valid = `{"operation_key":"owned-write","update_id":55}`
	for _, token := range []string{"", signer.Token("alice"), signer.DeliveryToken()} {
		assert.Equal(t, http.StatusUnauthorized, call(token, valid).Code)
	}
	assert.Equal(
		t,
		http.StatusBadRequest,
		call(
			signer.MemoryProvenanceToken("alice"),
			`{"operation_key":"owned-write","update_id":55,"owner":"bob"}`,
		).Code,
	)
	assert.Equal(
		t,
		http.StatusBadRequest,
		call(
			signer.MemoryProvenanceToken("alice"),
			`{"operation_key":"owned-write","update_id":55,"source_keys":["forged"]}`,
		).Code,
	)
	assert.Equal(
		t,
		http.StatusNotFound,
		call(signer.MemoryProvenanceToken("alice"), `{"operation_key":"owned-write","update_id":999}`).Code,
	)
	assert.Equal(t, http.StatusOK, call(signer.MemoryProvenanceToken("bob"), valid).Code)
	page, err := s.SearchMemory(t.Context(), "alice", knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	before, err := s.MemorySources(t.Context(), "alice", page.Entries[0].Ref)
	require.NoError(t, err)
	assert.Empty(t, before.Events)
	for range 2 {
		require.Equal(t, http.StatusOK, call(signer.MemoryProvenanceToken("alice"), valid).Code)
	}
	after, err := s.MemorySources(t.Context(), "alice", page.Entries[0].Ref)
	require.NoError(t, err)
	require.Len(t, after.Events, 1)
	assert.Equal(t, "Alice source", after.Events[0].Text)
}

func TestMemoryHostArchivePreservesSensitiveAndMediaSuppression(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.Handler(appservices.NewServices(s.DB, appservices.Options{}), signer, slog.New(slog.DiscardHandler))
	call := func(token, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	const path = "/internal/history/archive/original"
	const user = `{"source_key":"tg-user-56","kind":"user","text":"password: hidden-value"}`
	assert.Equal(t, http.StatusUnauthorized, call(signer.Token("alice"), path, user).Code)
	assert.Equal(t, http.StatusUnauthorized, call(signer.Token("alice"), "/internal/memory/assess", `{}`).Code)
	response := call(signer.MemoryProvenanceToken("alice"), path, user)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = call(
		signer.MemoryProvenanceToken("alice"),
		"/internal/history/archive/derived",
		`{"source_key":"tg-assistant-56","text":"The original secret response","reply_to_update_id":56,"expected_generation":0,"read_authorities":[]}`,
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	response = call(
		signer.MemoryProvenanceToken("alice"),
		"/internal/history/archive/derived",
		`{"source_key":"tg-assistant-57","text":"The raw media response","reply_to_update_id":57,"media":true,"expected_generation":0,"read_authorities":[]}`,
	)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	archive := conversation.Service{DB: s.DB}
	page, err := archive.Read(t.Context(), "alice", conversation.Query{Limit: conversation.MaxPage})
	require.NoError(t, err)
	require.Len(t, page.Events, 3)
	assert.Equal(t, "[media response; expiring content omitted]", page.Events[0].Text)
	assert.Equal(t, "[response to sensitive request omitted]", page.Events[1].Text)
	assert.Equal(t, "[sensitive text omitted]", page.Events[2].Text)
	assert.True(t, page.Events[2].Omitted)
	page, err = archive.Read(t.Context(), "bob", conversation.Query{Limit: conversation.MaxPage})
	require.NoError(t, err)
	assert.Empty(t, page.Events)
	response = call(
		signer.MemoryProvenanceToken("alice"),
		path,
		`{"source_key":"tg-user-58","kind":"user","text":"injected","owner":"bob"}`,
	)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}
