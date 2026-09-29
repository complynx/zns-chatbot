package agenthost

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func (h HistoryReader) ReadHistory(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.HistoryProposal,
	input *agent.Input,
) error {
	if input.Conversation == nil || input.Conversation.Remaining <= 0 {
		return errors.New("history read budget exhausted")
	}
	index, err := h.Store.ReserveHistory(ctx, owner, updateID)
	if err != nil {
		return err
	}
	const readCount = 4
	page, err := h.Domain.ConversationHistory(ctx, owner, conversation.Query{Before: p.Before, Limit: readCount})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	boundHistoryPage(&page)
	reads, err := h.Store.CompleteHistory(ctx, owner, updateID, index, page)
	if err != nil {
		return err
	}
	input.Conversation.Remaining = agent.MaxHistoryReads - len(reads)
	return SetModelHistoryPages(input, reads)
}

func boundHistoryPage(page *conversation.Page) {
	const maxReadBytes = 6 * 1024
	for len(page.Events) > 1 {
		raw, _ := json.Marshal(page)
		if len(raw) <= maxReadBytes {
			return
		}
		page.Events = page.Events[:len(page.Events)-1]
		page.More = true
		page.NextBefore = page.Events[len(page.Events)-1].ID
	}
	if len(page.Events) == 1 {
		raw, _ := json.Marshal(page)
		if len(raw) > maxReadBytes {
			boundHistoryEvent(&page.Events[0])
		}
	}
}
