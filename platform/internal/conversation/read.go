package conversation

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (s Service) knownActor(ctx context.Context, actor string) error {
	known, err := dbgen.New(s.DB).KnownActor(ctx, actor)
	if err != nil {
		return err
	}
	if !known {
		return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return nil
}

func (s Service) Read(ctx context.Context, actor string, q Query) (Page, error) {
	if q.Before < 0 || q.After < 0 || q.Limit < 1 || q.Limit > MaxPage {
		return Page{}, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidHistory}
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return Page{}, err
	}
	generation, err := s.Generation(ctx, actor)
	if err != nil {
		return Page{}, err
	}
	rows, err := dbgen.New(s.DB).ReadEvents(ctx, dbgen.ReadEventsParams{
		Owner: actor, BeforeID: q.Before, AfterID: q.After, PageLimit: int64(q.Limit + 1),
	})
	if err != nil {
		return Page{}, err
	}
	events := make([]Event, len(rows))
	for index, row := range rows {
		events[index] = Event(row)
	}
	result := Page{Events: events, More: len(events) > q.Limit, Generation: generation}
	if result.More {
		result.Events = result.Events[:q.Limit]
	}
	if len(result.Events) > 0 {
		result.NextBefore = result.Events[len(result.Events)-1].ID
	}
	return result, nil
}

func (s Service) Window(ctx context.Context, actor string, count int) (Window, error) {
	if count < 1 || count > MaxRecent {
		return Window{}, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidHistory}
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return Window{}, err
	}
	queries := dbgen.New(s.DB)
	generation, err := queries.HistoryGeneration(ctx, actor)
	if err != nil {
		return Window{}, err
	}
	rows, err := queries.RecentEvents(ctx, dbgen.RecentEventsParams{Owner: actor, RecentLimit: int64(count)})
	if err != nil {
		return Window{}, err
	}
	recent := make([]Event, len(rows))
	for index, row := range rows {
		recent[index] = Event(row)
	}
	result := Window{Recent: recent, Generation: generation}
	if len(recent) > 0 {
		result.BeforeID = recent[0].ID
	}
	summary, err := queries.ReadSummary(ctx, actor)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Window{}, err
	}
	result.Summary = Summary(summary)
	result.Gap, err = queries.HasSummaryGap(ctx, dbgen.HasSummaryGapParams{Owner: actor, ID: result.BeforeID})
	return result, err
}

// SummaryBatch returns only uncovered older events, including late commits with
// IDs below the displayed high-water mark. A bounded caller may take a prefix.
func (s Service) SummaryBatch(ctx context.Context, actor string, before int64) ([]Event, error) {
	if err := s.knownActor(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := dbgen.New(s.DB).SummaryEvents(ctx, dbgen.SummaryEventsParams{
		Owner: actor, BeforeID: before, PageLimit: MaxPage,
	})
	if err != nil {
		return nil, err
	}
	events := make([]Event, len(rows))
	for index, row := range rows {
		events[index] = Event(row)
	}
	return events, nil
}
