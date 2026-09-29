package agent

import (
	"context"
	"os"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// Opt-in synthetic reproduction; parent runs it with the existing CLI identity.
func TestLiveKnowledgeMemo(t *testing.T) {
	t.Parallel()
	executable := os.Getenv("ZNS_LIVE_CODEX_EXECUTABLE")
	if executable == "" {
		t.Skip("ZNS_LIVE_CODEX_EXECUTABLE enables this real-model reproduction")
	}
	model := Codex{Executable: executable, SyntheticOnly: true}
	input := Input{
		Text:     "Remember for me: my private test marker is amber pineapple.",
		Language: "ru",
		View:     KnowledgeView,
		Knowledge: &KnowledgeContext{
			Scopes:    []knowledge.Scope{{Phase: "general", CanReview: true}},
			Memos:     []knowledge.Memo{},
			Remaining: MaxKnowledgeReads,
		},
	}
	for range 2 {
		plan, err := planWithSkills(t.Context(), input, func(ctx context.Context, p providerPrompt) (string, error) {
			raw, callErr := model.structured(ctx, p)
			t.Logf("phase=%s response=%s error=%v", p.name, raw, callErr)
			return raw, callErr
		})
		require.NoError(t, err)
		require.NotNil(t, plan.KnowledgeAction, "clear remember request must choose its internal key and act")
		cyrillic := false
		for _, letter := range plan.Text {
			cyrillic = cyrillic || unicode.In(letter, unicode.Cyrillic)
		}
		assert.False(
			t,
			cyrillic,
			"English current question must not produce Russian conversational text: %s",
			plan.Text,
		)
		proposal := *plan.KnowledgeAction
		if proposal.Name == knowledge.MemoSet {
			assert.Empty(t, proposal.Event)
			assert.NotEmpty(t, proposal.FactKey)
			assert.Contains(t, proposal.Text, "amber pineapple")
			return
		}
		require.Equal(t, KnowledgeMemoRead, proposal.Name)
		input.Knowledge.Reads = append(
			input.Knowledge.Reads,
			KnowledgeReadResult{Request: proposal, Memo: &knowledge.Memo{Key: proposal.FactKey}},
		)
		input.Knowledge.Remaining--
	}
	t.Fatal("memo read was not followed by a save proposal")
}

func TestLiveKnowledgeScopedFacts(t *testing.T) {
	t.Parallel()
	executable := os.Getenv("ZNS_LIVE_CODEX_EXECUTABLE")
	if executable == "" {
		t.Skip("ZNS_LIVE_CODEX_EXECUTABLE enables this real-model reproduction")
	}
	model := Codex{Executable: executable, SyntheticOnly: true}
	input := Input{
		Text:     "What is the dress code for sandbox-past, and is there a step-free entrance?",
		Language: "ru",
		View:     KnowledgeView,
		Knowledge: &KnowledgeContext{
			Scopes:    []knowledge.Scope{{Phase: "general"}, {Event: "sandbox-past", Phase: "past"}},
			Remaining: MaxKnowledgeReads,
		},
	}
	facts := []knowledge.Fact{
		{
			Event:     "sandbox-past",
			Phase:     "past",
			Topic:     "venue",
			Key:       "dress_code",
			Text:      "The sandbox-past dress code was emerald green.",
			Active:    true,
			Untrusted: true,
		},
		{
			Phase:     "general",
			Topic:     "venue",
			Key:       "accessibility",
			Text:      "A step-free entrance is available through the north door.",
			Active:    true,
			Untrusted: true,
		},
	}
	seen := map[KnowledgeProposal]bool{}
	for attempt := 0; attempt <= MaxKnowledgeReads; attempt++ {
		plan, err := model.Plan(t.Context(), input)
		require.NoError(t, err)
		if plan.KnowledgeAction == nil {
			answer := strings.ToLower(plan.Text)
			assert.Contains(t, answer, "emerald")
			assert.Contains(t, answer, "north")
			return
		}
		p := *plan.KnowledgeAction
		require.Equal(t, KnowledgeRead, p.Name)
		require.False(t, seen[p], "do not spend the bounded budget repeating the same query")
		seen[p] = true
		result := KnowledgeReadResult{Request: p}
		for _, fact := range facts {
			if (fact.Event == "" || p.Event == fact.Event) && (p.Topic == "" || p.Topic == fact.Topic) &&
				(p.FactKey == "" || p.FactKey == fact.Key) && strings.Contains(strings.ToLower(fact.Text), strings.ToLower(p.Text)) {
				result.Facts = append(result.Facts, fact)
			}
		}
		input.Knowledge.Reads = append(input.Knowledge.Reads, result)
		input.Knowledge.Remaining--
	}
	t.Fatal("fact retrieval did not produce a grounded answer within the read budget")
}
