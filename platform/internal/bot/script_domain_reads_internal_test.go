package bot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
)

func TestDomainPageSnapshotAndByteBounds(t *testing.T) {
	t.Parallel()
	items := []string{strings.Repeat("<", 4000), strings.Repeat("🌍", 4000), "last"}
	cursor := scriptReadCursor{Owner: "alice", Kind: "massage.parties", Scope: "event"}
	first, err := scriptDomainItems(items, cursor, "")
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	require.True(t, first.More)
	cursor, err = readScriptCursor(first.NextCursor, "alice", "massage.parties", "event")
	require.NoError(t, err)
	next, err := scriptDomainItems(items, cursor, "")
	require.NoError(t, err)
	require.Equal(t, items[1:], next.Items)
	require.False(t, next.More)
	items[0] = "changed"
	_, err = scriptDomainItems(items, cursor, "")
	require.ErrorIs(t, err, appclient.ErrReadStale)
}

func TestDomainPageDiagnosticsMetadata(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, body string
		count      int
		empty      bool
	}{
		{"passes.events", `{"items":[],"more":false}`, 0, true},
		{"massage.slots", `{"items":[{"private":"never logged"}],"more":true}`, 1, false},
		{"passes.invitations", `{"items":[],"more":true}`, 0, false},
		{"history.page", `{"events":[],"more":false}`, 0, true},
		{"orders.page", `{"orders":[{}],"more":false}`, 1, false},
		{"massage.slots", `{"error":"stale","restart":true}`, 0, false},
		{"massage.slots", `invalid`, 0, false},
		{"passes.get", `{"items":[]}`, 0, false},
	} {
		count, empty := agenthost.ScriptToolResultMetadata(scenario.name, json.RawMessage(scenario.body))
		require.Equal(t, scenario.count, count)
		require.Equal(t, scenario.empty, empty)
	}
}
