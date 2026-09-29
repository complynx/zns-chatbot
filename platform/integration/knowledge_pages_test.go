package integration_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestKnowledgePagesPreserveOverridesAndCoverage(t *testing.T) {
	t.Parallel()
	s := knowledgeFixture(t)
	const total = knowledge.MaxResults + 3
	for index := range total {
		key := fmt.Sprintf("fact_%02d", index)
		knowledgeFact(t, s, "", key, "General fact", 0)
	}
	knowledgeFact(t, s, "kb-current", "fact_00", "Current override", 0)
	first, err := s.RetrievePage(t.Context(), "alice", knowledge.Query{Event: "kb-current"})
	require.NoError(t, err)
	require.Len(t, first.Facts, knowledge.MaxResults)
	assert.True(t, first.More)
	assert.Equal(t, "Current override", first.Facts[0].Text)
	second, err := s.RetrievePage(t.Context(), "alice", knowledge.Query{Event: "kb-current", Cursor: first.NextCursor})
	require.NoError(t, err)
	assert.False(t, second.More)
	require.Len(t, second.Facts, total-knowledge.MaxResults)
	seen := map[string]bool{}
	for _, fact := range append(first.Facts, second.Facts...) {
		assert.False(t, seen[fact.Key], "pages must not repeat a resolved fact")
		seen[fact.Key] = true
	}
	assert.Len(t, seen, total)
	_, err = s.RetrievePage(t.Context(), "alice", knowledge.Query{Cursor: "not-a-valid-cursor"})
	require.Error(t, err)
}
