package bot

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestKnowledgeToolsRejectAuthorityAndApproval(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, args string }{
		{"knowledge.curate", `{"topic":"travel","fact_key":"venue","text":"x","version":5}`},
		{"knowledge.suggest", `{"topic":"travel","fact_key":"venue","text":"x","owner":"bob"}`},
		{"knowledge.memo_set", `{"fact_key":"diet","text":"x","key":"chosen"}`},
		{"knowledge.review", `{"proposal_id":1,"decision":"approve"}`},
		{"knowledge.review_card", `{"proposal_id":1,"decision":"approve"}`},
		{"knowledge.memo_read", `{"event":"foreign","fact_key":"diet"}`},
		{"knowledge.read", `{"fact_key":"venue"}`},
		{"knowledge.proposals", `{"review_queue":true}`},
	}
	for _, tc := range cases {
		t.Run(tc.name+tc.args, func(t *testing.T) {
			t.Parallel()
			_, _, err := knowledgeToolProposal(
				scriptclient.ToolCall{Name: tc.name, Arguments: json.RawMessage(tc.args)},
			)
			assert.Error(t, err)
		})
	}
}

func TestKnowledgeToolsKeepTypedValidation(t *testing.T) {
	t.Parallel()
	proposal, cursor, err := knowledgeToolProposal(
		scriptclient.ToolCall{
			Name:      scriptKnowledgeRead,
			Arguments: json.RawMessage(`{"event":"festival","topic":"travel","text":"venue","cursor":"page"}`),
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "read", proposal.Name)
	assert.Equal(t, "venue", proposal.Text)
	assert.Equal(t, "page", cursor)
	assert.Empty(t, proposal.Cursor, "outer script pagination is not a domain cursor")
}
