package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// A recovered receipt renders catalog text, never the stored knowledge body.
// Pending filtering returns to the normal privacy-checked continuation instead
// of completing an unfinished suggestion or calling a classifier in lookup.
func (b *Bot) knowledgeReceiptNotice(
	ctx context.Context,
	in incoming,
	command knowledge.Command,
	result knowledge.Result,
) (string, bool, error) {
	if !result.Redacted && command.Name == knowledge.Suggest && result.Proposal != nil &&
		result.Proposal.State == knowledgePendingFilter {
		return "", false, nil
	}
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", false, err
	}
	notice := i18n.KnowledgeSaved
	switch {
	case result.Redacted:
		notice = i18n.KnowledgeUnavailable
	case command.Name == knowledge.Suggest:
		notice = i18n.KnowledgeSuggested
	}
	if err = b.saveKnowledgeView(
		ctx,
		in.owner,
		knowledgeView{Event: command.Event, Mode: knowledgeCommandMode(command.Name)},
	); err != nil {
		return "", false, err
	}
	text, err := i18n.Translate(preference.Language, notice, nil)
	return text, err == nil, err
}
