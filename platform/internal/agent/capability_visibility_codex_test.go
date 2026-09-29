package agent_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func codexCapabilityHelper(prompt string, schema []byte) int {
	for _, hidden := range []string{"proof_accept", "proof_reject", "admin_assign", "admin_uncouple", "payment_queue", "review_card", "remove_fact"} {
		if strings.Contains(prompt, hidden) || bytes.Contains(schema, []byte(hidden)) {
			return 1
		}
	}
	response := `{"lineup_action":null,"text":"visibility checked","view":"passes","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null,"script_action":null,"history_action":null,"registration_action":null}`
	if bytes.Contains(schema, []byte(`"skills"`)) {
		response = `{"skills":["registration","knowledge"],"reply_language":"en"}`
	}
	_, err := os.Stdout.WriteString(codexStream(response))
	if err != nil {
		return 1
	}
	return 0
}

func TestCodexWireOmitsUnavailableTools(t *testing.T) {
	t.Parallel()
	executable, err := os.Executable()
	require.NoError(t, err)
	plan, err := (agent.Codex{Executable: executable, SyntheticOnly: true}).Plan(
		t.Context(),
		agent.Input{Text: "capability-visibility"},
	)
	require.NoError(t, err)
	assert.Equal(t, "visibility checked", plan.Text)
}
