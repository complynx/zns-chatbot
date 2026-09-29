package bot

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestModelToolAuthorityArguments(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"model":"gpt-6-sol","effort":"high","version":9}`,
		`{"model":"gpt-6-sol","effort":"high","operation_key":"forged"}`,
		`{"model":"gpt-6-sol","effort":"high","owner":"bob"}`,
		`{"model":"gpt-6-sol"}`,
		`{"model":"invalid","effort":"high"}`,
	} {
		_, err := decodeModelTool(scriptclient.ToolCall{Name: "models.own.set", Arguments: json.RawMessage(raw)})
		require.Error(t, err)
	}
	_, err := decodeModelTool(
		scriptclient.ToolCall{
			Name:      "models.grants.set",
			Arguments: json.RawMessage(`{"owner":"alice","capability":"own"}`),
		},
	)
	require.Error(t, err)
	for _, owner := range []string{"", "*", "alice"} {
		_, err = modelToolEndpoint("models.others.set", "alice", owner)
		require.Error(t, err)
	}
}
