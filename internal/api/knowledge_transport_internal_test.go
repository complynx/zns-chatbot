package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/knowledge"
)

func TestKnowledgeMaximumContentFitsEscapedRequest(t *testing.T) {
	t.Parallel()
	prefix := "https://example.com/"
	content := knowledge.Content{
		Slug:     "max-document",
		Title:    strings.Repeat("<", knowledge.MaxTitleRunes),
		Language: "en",
		Body:     strings.Repeat("<", knowledge.MaxBodyBytes),
	}
	for range knowledge.MaxSources {
		content.Sources = append(
			content.Sources,
			knowledge.Source{
				Label: strings.Repeat("<", knowledge.MaxTitleRunes),
				URL:   prefix + strings.Repeat("<", knowledge.MaxSourceURLBytes-len(prefix)),
			},
		)
	}
	normalized, err := content.Normalize()
	require.NoError(t, err)
	encoded, err := json.Marshal(
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "00000000-0000-4000-8000-000000000001",
			"arguments": knowledgeEditArguments{
				Content: normalized,
				Reason:  "Publish the complete requested content",
			},
		},
	)
	require.NoError(t, err)
	require.LessOrEqual(
		t,
		len(encoded),
		knowledgeTransportLimit,
		"valid content must remain sendable after JSON escaping",
	)
}
