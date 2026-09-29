package agenthost

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

// HistoryDomain supplies current owner-authorized archive projections.
type HistoryDomain interface {
	ConversationWindow(context.Context, string, int) (conversation.Window, error)
	ConversationHistory(context.Context, string, conversation.Query) (conversation.Page, error)
	HistoryGeneration(context.Context, string) (int64, error)
}

// HistorySummaries keeps summary reads and commits in the conversation domain.
type HistorySummaries interface {
	HistorySummaryBatch(context.Context, string, int64) ([]conversation.Event, error)
	CommitHistorySummary(context.Context, string, int64, []int64, string) error
}

type HistoryReadStore interface {
	History(context.Context, string, int64) ([]conversation.Page, error)
	ReserveHistory(context.Context, string, int64) (int, error)
	CompleteHistory(context.Context, string, int64, int, conversation.Page) ([]conversation.Page, error)
	ReserveHistorySummary(context.Context, string, int64) error
}

// HistoryReader owns summary selection, read budgets and model-facing projection.
type HistoryReader struct {
	Domain    HistoryDomain
	Summaries HistorySummaries
	Authority SourceAuthority
	Store     HistoryReadStore
	Model     agent.Model
	Limit     int
}

func (h HistoryReader) InitialHistory(ctx context.Context, owner string) (conversation.Window, error) {
	count := h.Limit
	if count == 0 {
		count = conversation.DefaultRecent
	}
	return h.Domain.ConversationWindow(ctx, owner, count)
}

func (h HistoryReader) History(
	ctx context.Context,
	owner string,
	updateID int64,
	input *agent.Input,
	window conversation.Window,
) error {
	var err error
	if window.Gap {
		if err = h.refreshHistorySummary(ctx, owner, updateID, window); ctx.Err() != nil {
			return ctx.Err()
		}
		// A failed/unavailable summary leaves its explicit gap and does not break the request.
		if err == nil {
			window, err = h.InitialHistory(ctx, owner)
			if err != nil {
				return err
			}
		}
	}
	reads, err := h.Store.History(ctx, owner, updateID)
	if err != nil {
		return err
	}
	input.ReadAuthorities, err = MergeReadAuthorities(window.Summary.ReadAuthorities)
	if err != nil {
		return err
	}
	input.HistoryGeneration = window.Generation
	RedactHistoryPages(reads, window.Generation)
	input.Conversation = &agent.HistoryContext{
		Summary:   window.Summary,
		Gap:       window.Gap,
		BeforeID:  window.BeforeID,
		Remaining: agent.MaxHistoryReads - len(reads),
		Reads:     reads,
	}
	if err = SetModelHistoryPages(input, reads); err != nil {
		return err
	}
	input.History = make([]agent.Event, 0, len(window.Recent))
	for _, event := range window.Recent {
		input.ReadAuthorities, err = MergeReadAuthorities(input.ReadAuthorities, event.ReadAuthorities)
		if err != nil {
			return err
		}
		event.ReadAuthorities = nil
		boundHistoryEvent(&event)
		raw, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			return marshalErr
		}
		kind := event.Kind
		switch kind {
		case "domain":
			kind = "result"
		case "system":
			kind = "order_notification"
		}
		input.History = append(input.History, agent.Event{Kind: kind, Content: raw})
	}
	return nil
}

func (h HistoryReader) refreshHistorySummary(
	ctx context.Context,
	owner string,
	updateID int64,
	window conversation.Window,
) error {
	summarizer, ok := h.Model.(agent.HistorySummarizer)
	if !ok {
		return errors.New("history summarizer unavailable")
	}
	if err := h.Store.ReserveHistorySummary(ctx, owner, updateID); err != nil {
		return err
	}
	events, err := h.Summaries.HistorySummaryBatch(ctx, owner, window.BeforeID)
	if err != nil || len(events) == 0 {
		return err
	}
	input := agent.HistorySummaryInput{Previous: window.Summary.Text}
	authorities, err := MergeReadAuthorities(window.Summary.ReadAuthorities)
	if err != nil {
		return err
	}
	var ids []int64
	for _, event := range events {
		eventAuthorities := event.ReadAuthorities
		event.ReadAuthorities = nil
		input.Events = append(input.Events, event)
		data, marshalErr := json.Marshal(input)
		if marshalErr != nil {
			return marshalErr
		}
		if len(data) > agent.MaxSummaryInputBytes {
			input.Events = input.Events[:len(input.Events)-1]
			break
		}
		authorities, err = MergeReadAuthorities(authorities, eventAuthorities)
		if err != nil {
			return err
		}
		ids = append(ids, event.ID)
	}
	if len(ids) == 0 {
		return errors.New("summary batch exceeds budget")
	}
	input.BeforeProvider = h.summaryGuard(owner, window.Generation, authorities)
	if err = input.BeforeProvider(ctx); err != nil {
		return err
	}
	text, err := summarizer.SummarizeHistory(ctx, input)
	if err != nil {
		return err
	}
	return h.Summaries.CommitHistorySummary(ctx, owner, window.Summary.Version, ids, text)
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

// summaryGuard binds every actual request to the generation and sources of the
// selected summary input. It never refreshes or replaces that input.
func (h HistoryReader) summaryGuard(
	owner string,
	generation int64,
	authorities []readsource.Authority,
) func(context.Context) error {
	return func(ctx context.Context) error {
		return h.checkSnapshot(ctx, owner, generation, authorities)
	}
}
