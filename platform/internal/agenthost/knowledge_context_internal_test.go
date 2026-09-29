package agenthost

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestKnowledgeReadBoundRetainsRequestAndOmission(t *testing.T) {
	t.Parallel()
	read := agent.KnowledgeReadResult{
		Request: agent.KnowledgeProposal{Name: agent.KnowledgeRead, Event: "festival"},
		Facts:   []knowledge.Fact{{Text: strings.Repeat("x", knowledge.MaxText)}},
	}
	boundKnowledgeRead(&read, 512)
	assert.Empty(t, read.Facts)
	assert.True(t, read.Omitted)
	assert.Equal(t, "festival", read.Request.Event)
}
