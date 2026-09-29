package integration_test

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func requireMemoryByteBound(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	require.LessOrEqual(t, len(encoded), knowledge.MemoryResponseBytes)
}

func TestMemoryEscapedBytePagesPreserveEveryMatch(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	const total = 20
	topic := strings.Repeat("a", 100)
	for i := range total {
		_, err := s.Execute(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.DocumentSet,
				Key:     fmt.Sprint("budget-", i),
				Topic:   topic,
				FactKey: fmt.Sprintf("%0100d", i),
				Text:    strings.Repeat("\u0001", 160) + " findme",
			},
		)
		require.NoError(t, err)
	}
	for _, query := range []knowledge.MemoryQuery{
		{Namespace: knowledge.MemoryPrivate},
		{Namespace: knowledge.MemoryPrivate, Text: "\u0001", Mode: knowledge.MemoryLiteral},
		{Namespace: knowledge.MemoryPrivate, Text: "\u0001", Mode: knowledge.MemoryRegex},
		{Namespace: knowledge.MemoryPrivate, Text: "findme", Mode: knowledge.MemoryText},
	} {
		seen := map[string]bool{}
		pages := 0
		for {
			page, err := s.SearchMemory(t.Context(), "alice", query)
			require.NoError(t, err)
			requireMemoryByteBound(t, page)
			require.NotEmpty(t, page.Entries)
			for _, entry := range page.Entries {
				require.False(t, seen[entry.Ref])
				seen[entry.Ref] = true
			}
			pages++
			if !page.More {
				break
			}
			require.NotEmpty(t, page.NextCursor)
			query.Cursor = page.NextCursor
		}
		assert.Greater(t, pages, 1, "byte budget must page before the count budget")
		assert.Len(t, seen, total)
	}
}

func TestMemoryEscapedHistorySummaryAndSourceBounds(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	key := strings.Repeat("k", 100)
	const total = 20
	for i := range total {
		_, err := s.Execute(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.DocumentSet,
				Key:     fmt.Sprint("revision-", i),
				Topic:   "summary",
				FactKey: key,
				Text:    strings.Repeat("\u0001", knowledge.MaxDocumentText),
				Version: int64(i),
			},
		)
		require.NoError(t, err)
	}
	page, err := s.SearchMemory(t.Context(), "alice", knowledge.MemoryQuery{Namespace: knowledge.MemoryPrivate})
	require.NoError(t, err)
	require.Len(t, page.Entries, 1)
	ref := page.Entries[0].Ref
	current, err := s.ReadMemory(t.Context(), "alice", ref)
	require.NoError(t, err)
	requireMemoryByteBound(t, current)
	assert.True(t, current.More)
	historical, err := s.ReadMemoryRevisionPage(t.Context(), "alice", ref, "")
	require.NoError(t, err)
	requireMemoryByteBound(t, historical)
	cursor := ""
	versions := map[int64]bool{}
	for {
		history, historyErr := s.MemoryHistory(t.Context(), "alice", ref, cursor)
		require.NoError(t, historyErr)
		requireMemoryByteBound(t, history)
		for _, entry := range history.Entries {
			require.False(t, versions[entry.Version])
			versions[entry.Version] = true
		}
		if !history.More {
			break
		}
		require.NotEmpty(t, history.NextCursor)
		cursor = history.NextCursor
	}
	assert.Len(t, versions, total)
	for i := 1; i < total; i++ {
		_, err = s.Execute(
			t.Context(),
			"alice",
			knowledge.Command{
				Name:    knowledge.DocumentSet,
				Key:     fmt.Sprint("summary-", i),
				Topic:   "summary",
				FactKey: fmt.Sprintf("%0100d", i),
				Text:    strings.Repeat("\u0001", 160),
			},
		)
		require.NoError(t, err)
	}
	overview, err := s.MemorySummary(t.Context(), "alice", knowledge.MemoryQuery{})
	require.NoError(t, err)
	requireMemoryByteBound(t, overview)
	assert.True(t, overview.More)
	archive := conversation.Service{DB: s.DB}
	keys := make([]string, 0, total)
	for i := range total {
		source := fmt.Sprint("source-", i)
		require.NoError(
			t,
			archive.Append(t.Context(), "alice", source, "user", strings.Repeat("\u0001", conversation.MaxTextBytes)),
		)
		keys = append(keys, source)
	}
	for start := 0; start < total; start += 8 {
		require.NoError(t, s.AttachMemorySources(t.Context(), "alice", "revision-19", keys[start:min(start+8, total)]))
	}
	sources, err := s.MemorySources(t.Context(), "alice", ref)
	require.NoError(t, err)
	require.Len(t, sources.Events, total)
	requireMemoryByteBound(t, sources)
}

func TestMemoryMaximumEscapedDocumentCrossesPublicAPI(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	handler := api.Handler(runtimeapp.NewServices(s.DB, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler))
	command := knowledge.Command{
		Name:    knowledge.DocumentSet,
		Key:     "escaped-max",
		Topic:   "notes",
		FactKey: "large",
		Text:    strings.Repeat("\u0001", knowledge.MaxDocumentText),
	}
	data, err := json.Marshal(command)
	require.NoError(t, err)
	const oldLimit = 65536
	require.Greater(t, len(data), oldLimit)
	request := httptest.NewRequest(http.MethodPost, "/v1/knowledge/actions", strings.NewReader(string(data)))
	request.Header.Set("Authorization", "Bearer "+signer.Token("alice"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var result knowledge.Result
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	require.NotNil(t, result.Document)
	assert.Equal(t, command.Text, result.Document.Text)
}
