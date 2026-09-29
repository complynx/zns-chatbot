package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

const historyReadInterrupted = "interrupted"

func (b *Bot) performContextRead(
	ctx context.Context,
	owner string,
	updateID int64,
	plan agent.Plan,
	input *agent.Input,
) (bool, error) {
	if plan.LineupAction != nil {
		return true, b.performLineupRead(*plan.LineupAction, input)
	}
	if p := plan.RegistrationAction; p != nil && p.Name == agent.RegistrationRead {
		return true, b.performRegistrationRead(ctx, owner, updateID, *p, input)
	}
	if plan.HistoryAction != nil {
		return true, b.performHistoryRead(ctx, owner, updateID, *plan.HistoryAction, input)
	}
	return b.performReadTool(ctx, owner, updateID, plan, input)
}

func (b *Bot) performHistoryRead(
	ctx context.Context,
	owner string,
	updateID int64,
	p agent.HistoryProposal,
	input *agent.Input,
) error {
	if input.Conversation == nil || input.Conversation.Remaining <= 0 {
		return errors.New("history read budget exhausted")
	}
	index, err := b.reserveHistoryRead(ctx, owner, updateID)
	if err != nil {
		return err
	}
	const readCount = 4
	page, err := b.API.ConversationHistory(ctx, owner, conversation.Query{Before: p.Before, Limit: readCount})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	boundHistoryPage(&page)
	reads, err := b.finishHistoryRead(ctx, owner, updateID, index, page)
	if err != nil {
		return err
	}
	input.Conversation.Reads = reads
	input.Conversation.Remaining = agent.MaxHistoryReads - len(reads)
	return nil
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

func (b *Bot) reserveHistoryRead(ctx context.Context, owner string, updateID int64) (int, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = historyReadLock(ctx, tx, owner, updateID); err != nil {
		return 0, err
	}
	var reads []conversation.Page
	err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`, owner, updateID).
		Scan(&reads)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	if len(reads) >= agent.MaxHistoryReads {
		return 0, errors.New("history read budget exhausted")
	}
	index := len(reads)
	reads = append(reads, conversation.Page{Events: []conversation.Event{}, Error: historyReadInterrupted})
	_, err = tx.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,'history_reads',$3)
 ON CONFLICT(owner,update_id,kind) DO UPDATE SET content=excluded.content`, owner, updateID, reads)
	if err != nil {
		return 0, err
	}
	return index, tx.Commit(ctx)
}

func (b *Bot) finishHistoryRead(
	ctx context.Context,
	owner string,
	updateID int64,
	index int,
	page conversation.Page,
) ([]conversation.Page, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = historyReadLock(ctx, tx, owner, updateID); err != nil {
		return nil, err
	}
	var reads []conversation.Page
	if err = tx.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`, owner, updateID).
		Scan(&reads); err != nil {
		return nil, err
	}
	if index >= len(reads) {
		return nil, errors.New("history reservation missing")
	}
	generation, generationErr := b.API.historyGeneration(ctx, owner)
	if generationErr != nil {
		return nil, generationErr
	}
	reads[index] = page
	redactHistoryPages(reads, generation)
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.interactions SET content=$3 WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`,
		owner,
		updateID,
		reads,
	)
	if err != nil {
		return nil, err
	}
	return reads, tx.Commit(ctx)
}
