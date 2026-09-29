package core_test

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestHistoryTransportChunksKeepEnvelopeAndCursorBounds(t *testing.T) {
	t.Parallel()
	value := strings.Repeat("\x00<&Ж😀\\\"", 100000)
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.Greater(t, len(data), core.ReadResourceBytes)
	cursor := core.ReadCursor{Actor: "alice", Scope: "orders.history.read:event:1"}
	_, err = core.JSONReadChunk(value, cursor)
	require.Error(t, err, "ordinary resource cap remains unchanged")
	agentPage, err := core.JSONReadDataChunk(data, cursor)
	require.NoError(t, err)
	assert.Equal(t, 4000, utf8.RuneCountInString(agentPage.JSON))
	var restored strings.Builder
	for {
		page, chunkErr := core.JSONReadTransportChunk(data, cursor)
		require.NoError(t, chunkErr)
		encoded, encodeErr := json.Marshal(page)
		require.NoError(t, encodeErr)
		assert.Less(t, len(encoded), 512<<10)
		assert.True(t, utf8.ValidString(page.JSON))
		restored.WriteString(page.JSON)
		if !page.More {
			break
		}
		cursor, err = core.DecodeReadCursor(page.NextCursor, "alice", cursor.Scope)
		require.NoError(t, err)
		_, err = core.DecodeReadCursor(page.NextCursor, "bob", cursor.Scope)
		require.Error(t, err)
	}
	assert.Equal(t, string(data), restored.String(), "all original escaped JSON bytes must survive paging")
	_, err = core.JSONReadTransportChunk([]byte(`"changed"`), cursor)
	require.Error(t, err, "a changed snapshot must reject continuation")
}
