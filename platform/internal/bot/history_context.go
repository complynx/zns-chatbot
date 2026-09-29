package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func (b *Bot) addHistoryContext(ctx context.Context, owner string, updateID int64, input *agent.Input) error {
	count := b.HistoryLimit
	if count == 0 {
		count = conversation.DefaultRecent
	}
	window, err := b.API.ConversationWindow(ctx, owner, count)
	if err != nil {
		return err
	}
	if window.Gap {
		if err = b.refreshHistorySummary(ctx, owner, updateID, window); ctx.Err() != nil {
			return ctx.Err()
		}
		// A failed/unavailable summary leaves its explicit gap and does not break the request.
		if err == nil {
			window, err = b.API.ConversationWindow(ctx, owner, count)
			if err != nil {
				return err
			}
		}
	}
	reads, err := b.historyReads(ctx, owner, updateID)
	if err != nil {
		return err
	}
	input.HistoryGeneration = window.Generation
	redactHistoryPages(reads, window.Generation)
	input.Conversation = &agent.HistoryContext{
		Summary:   window.Summary,
		Gap:       window.Gap,
		BeforeID:  window.BeforeID,
		Remaining: agent.MaxHistoryReads - len(reads),
		Reads:     reads,
	}
	input.History = make([]agent.Event, 0, len(window.Recent))
	for _, event := range window.Recent {
		boundHistoryEvent(&event)
		raw, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		kind := event.Kind
		switch kind {
		case "domain":
			kind = "result"
		case historySystemKind:
			kind = "order_notification"
		}
		input.History = append(input.History, agent.Event{Kind: kind, Content: raw})
	}
	return nil
}

func (b *Bot) refreshHistorySummary(
	ctx context.Context,
	owner string,
	updateID int64,
	window conversation.Window,
) error {
	summarizer, ok := b.Model.(agent.HistorySummarizer)
	if !ok {
		return errors.New("history summarizer unavailable")
	}
	var claimed bool
	err := b.DB.QueryRow(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,'history_summary_attempt','{}')
 ON CONFLICT DO NOTHING RETURNING true`, owner, updateID).
		Scan(&claimed)
	if err != nil {
		return err
	}

	events, err := b.API.HistorySummaryBatch(ctx, owner, window.BeforeID)
	if err != nil || len(events) == 0 {
		return err
	}
	input := agent.HistorySummaryInput{Previous: window.Summary.Text}
	var ids []int64
	for _, event := range events {
		input.Events = append(input.Events, event)
		data, marshalErr := json.Marshal(input)
		if marshalErr != nil {
			return marshalErr
		}
		if len(data) > agent.MaxSummaryInputBytes {
			input.Events = input.Events[:len(input.Events)-1]
			break
		}
		ids = append(ids, event.ID)
	}
	if len(ids) == 0 {
		return errors.New("summary batch exceeds budget")
	}
	generation, err := b.API.historyGeneration(ctx, owner)
	if err != nil {
		return err
	}
	if generation != window.Generation {
		return errScriptReadStale
	}
	text, err := summarizer.SummarizeHistory(ctx, input)
	if err != nil {
		return err
	}
	return b.API.CommitHistorySummary(ctx, owner, window.Summary.Version, ids, text)
}

// The archive retains bounded original text. Model snippets mark omission rather
// than pretending an excerpt is a semantic summary of the missing content.
func boundHistoryEvent(event *conversation.Event) {
	const maxSnippetBytes = 512
	for {
		raw, _ := json.Marshal(event)
		if len(raw) <= maxSnippetBytes || event.Text == "" {
			return
		}
		text := []rune(event.Text)
		event.Text = string(text[:len(text)/2])
		event.Omitted = true
		if event.OmissionReason == "" {
			event.OmissionReason = "excerpt"
		}
	}
}

func (b *Bot) historyReads(ctx context.Context, owner string, updateID int64) ([]conversation.Page, error) {
	var reads []conversation.Page
	err := b.DB.QueryRow(ctx, `SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='history_reads'`, owner, updateID).
		Scan(&reads)
	if errors.Is(err, pgx.ErrNoRows) {
		return []conversation.Page{}, nil
	}
	if err != nil {
		return nil, err
	}
	generation, err := b.API.historyGeneration(ctx, owner)
	if err != nil {
		return nil, err
	}
	if err = b.reconcileHistoryCaches(ctx, owner, generation); err != nil {
		return nil, err
	}
	redactHistoryPages(reads, generation)
	return reads, nil
}

func historyReadLock(ctx context.Context, tx pgx.Tx, owner string, updateID int64) error {
	_, err := tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		"history-read:"+owner+":"+strconv.FormatInt(updateID, 10),
	)
	return err
}
