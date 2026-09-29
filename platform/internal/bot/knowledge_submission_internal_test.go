package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestKnowledgeSubmissionRequiresOwnerDraftCard(t *testing.T) {
	t.Parallel()
	proposal := knowledge.Proposal{
		ID:      42,
		Version: 3,
		Owner:   "author",
		State:   knowledge.AwaitingSubmission,
		Event:   "festival",
		Topic:   "travel",
		FactKey: "entrance",
		Text:    "The north entrance opens at 09:00.\nВход с севера.",
	}
	submission := knowledgeProposalSubmission("author", proposal, false)
	require.NotNil(t, submission)
	assert.Equal(
		t,
		knowledge.Submission{
			ProposalID: 42,
			Version:    3,
			Event:      "festival",
			Topic:      "travel",
			FactKey:    "entrance",
			Text:       proposal.Text,
		},
		*submission,
	)
	assert.Nil(t, knowledgeProposalSubmission("other", proposal, false))
	assert.Nil(t, knowledgeProposalSubmission("author", proposal, true))
	for _, state := range []string{"pending_filter", "pending_review", "filtered", "approved", "rejected"} {
		proposal.State = state
		assert.Nil(t, knowledgeProposalSubmission("author", proposal, false), state)
	}
}

func TestKnowledgeProposalCardShowsExactBodyAndPrivateSubmissionState(t *testing.T) {
	t.Parallel()
	proposal := knowledge.Proposal{
		State:   knowledge.AwaitingSubmission,
		Event:   "festival",
		Topic:   "travel",
		FactKey: "entrance",
		Text:    "First line <exact>\nSecond line & unchanged",
		Reason:  "Private classifier explanation",
	}
	for _, locale := range []struct{ language, disclosure string }{
		{"en", "Reviewers have not received it."}, {"ru", "Проверяющие ещё не получили его."},
	} {
		renderer := knowledgeRenderer{language: locale.language}
		text := renderer.proposalText(proposal, false)
		assert.Contains(t, text, "festival · travel / entrance")
		assert.Contains(t, text, "\n\n"+proposal.Text+"\n\n")
		assert.Contains(t, text, locale.disclosure)
		proposal.State = "pending_review"
		reviewed := renderer.proposalText(proposal, true)
		assert.NotContains(t, reviewed, proposal.Reason)
		assert.Contains(t, reviewed, proposal.Text)
		assert.NotContains(t, reviewed, locale.disclosure)
		proposal.State = knowledge.AwaitingSubmission
	}
}
