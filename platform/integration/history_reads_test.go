package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func TestHistoryReadEmptyAndPaginationContracts(t *testing.T) {
	t.Parallel()
	f := setup(t)
	service := conversation.Service{DB: f.db}
	page, err := service.Read(t.Context(), "alice", conversation.Query{Limit: 1})
	require.NoError(t, err)
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	assert.JSONEq(t, `{"events":[],"next_before":0,"more":false}`, string(encoded))
	window, err := service.Window(t.Context(), "alice", 1)
	require.NoError(t, err)
	assert.NotNil(t, window.Recent)
	assert.Empty(t, window.Recent)
	assert.Equal(t, conversation.Summary{}, window.Summary)
	assert.False(t, window.Gap)
	batch, err := service.SummaryBatch(t.Context(), "alice", 0)
	require.NoError(t, err)
	assert.NotNil(t, batch)
	assert.Empty(t, batch)

	require.NoError(t, service.Append(t.Context(), "alice", "first", "user", "First message"))
	require.NoError(t, service.Append(t.Context(), "bob", "other", "user", "Other owner"))
	require.NoError(t, service.Append(t.Context(), "alice", "second", "assistant", "Second message"))
	page, err = service.Read(t.Context(), "alice", conversation.Query{Limit: 1})
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	assert.True(t, page.More)
	assert.Equal(t, "Second message", page.Events[0].Text)
	assert.False(t, page.Events[0].At.IsZero())
	assert.JSONEq(t, `{}`, string(page.Events[0].Details))
	older, err := service.Read(t.Context(), "alice", conversation.Query{Before: page.NextBefore, Limit: 1})
	require.NoError(t, err)
	require.Len(t, older.Events, 1)
	assert.False(t, older.More)
	assert.Equal(t, "First message", older.Events[0].Text)
	newer, err := service.Read(t.Context(), "alice", conversation.Query{After: older.NextBefore, Limit: 1})
	require.NoError(t, err)
	assert.Equal(t, page.Events, newer.Events)
	window, err = service.Window(t.Context(), "alice", 2)
	require.NoError(t, err)
	require.Len(t, window.Recent, 2)
	assert.Equal(t, older.Events[0], window.Recent[0])
	assert.Equal(t, page.Events[0], window.Recent[1])
}
