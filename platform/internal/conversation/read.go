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
	state, err := s.authoritySnapshot(
		ctx,
		actor,
		nil,
		authorityScope{page: Query{Before: q.Before, After: q.After, Limit: q.Limit + 1}},
	)
	if err != nil {
		return Page{}, err
	}
	defer func() { _ = state.tx.Rollback(ctx) }()
	queries := dbgen.New(state.tx)
	generation, err := queries.HistoryGeneration(ctx, actor)
	if err != nil {
		return Page{}, err
	}
	rows, err := queries.ReadEvents(ctx, dbgen.ReadEventsParams{
		Owner: actor, BeforeID: q.Before, AfterID: q.After, PageLimit: int64(q.Limit + 1),
	})
	if err != nil {
		return Page{}, err
	}
	events := make([]Event, len(rows))
	for index, row := range rows {
		events[index] = eventFromRow(row, state.byEvent[row.ID])
	}
	result := Page{Events: events, More: len(events) > q.Limit, Generation: generation}
	if result.More {
		result.Events = result.Events[:q.Limit]
	}
	if len(result.Events) > 0 {
		result.NextBefore = result.Events[len(result.Events)-1].ID
	}
	return boundedHistoryResult(result, state.tx.Commit(ctx))
}

func (s Service) Window(ctx context.Context, actor string, count int) (Window, error) {
	if count < 1 || count > MaxRecent {
		return Window{}, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidHistory}
	}
	state, err := s.authoritySnapshot(ctx, actor, nil, authorityScope{page: Query{Limit: count}, summary: true})
	if err != nil {
		return Window{}, err
	}
	defer func() { _ = state.tx.Rollback(ctx) }()
	queries := dbgen.New(state.tx)
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
		recent[index] = eventFromRow(dbgen.ReadEventsRow(row), state.byEvent[row.ID])
	}
	result := Window{Recent: recent, Generation: generation}
	if len(recent) > 0 {
		result.BeforeID = recent[0].ID
	}
	summary, err := queries.ReadSummary(ctx, actor)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Window{}, err
	}
	result.Summary = Summary{Version: summary.Version, ThroughID: summary.ThroughID, Text: summary.Text}
	result.Summary.ReadAuthorities, err = state.summaryAuthorities(nil)
	if err != nil {
		return Window{}, err
	}
	result.Gap, err = queries.HasSummaryGap(ctx, dbgen.HasSummaryGapParams{Owner: actor, ID: result.BeforeID})
	if err != nil {
		return Window{}, err
	}
	return boundedHistoryResult(result, state.tx.Commit(ctx))
}

// SummaryBatch returns only uncovered older events, including late commits with
// IDs below the displayed high-water mark. A bounded caller may take a prefix.
func (s Service) SummaryBatch(ctx context.Context, actor string, before int64) ([]Event, error) {
	if before < 0 {
		return nil, invalidHostArchive()
	}
	state, err := s.authoritySnapshot(ctx, actor, nil, authorityScope{batchBefore: before})
	if err != nil {
		return nil, err
	}
	defer func() { _ = state.tx.Rollback(ctx) }()
	rows, err := dbgen.New(state.tx).SummaryEventsByIDs(ctx, dbgen.SummaryEventsByIDsParams{
		Owner: actor, Column2: state.ids,
	})
	if err != nil {
		return nil, err
	}
	events := make([]Event, len(rows))
	for index, row := range rows {
		events[index] = eventFromRow(dbgen.ReadEventsRow(row), state.byEvent[row.ID])
	}
	return boundedHistoryResult(events, state.tx.Commit(ctx))
}

// ReadSelected returns bounded owner-bound source messages through the same
// authority transaction used by ordinary history pages and full-body reads.
func (s Service) ReadSelected(ctx context.Context, actor string, ids []int64) (Page, error) {
	if len(ids) > MaxPage {
		return Page{}, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidHistory}
	}
	state, err := s.authoritySnapshot(ctx, actor, nil, authorityScope{ids: ids})
	if err != nil {
		return Page{}, err
	}
	defer func() { _ = state.tx.Rollback(ctx) }()
	queries := dbgen.New(state.tx)
	generation, err := queries.HistoryGeneration(ctx, actor)
	if err != nil {
		return Page{}, err
	}
	rows, err := queries.ReadSelectedEvents(ctx, dbgen.ReadSelectedEventsParams{Owner: actor, Column2: ids})
	if err != nil {
		return Page{}, err
	}
	result := Page{Events: make([]Event, 0, len(rows)), Generation: generation}
	for _, row := range rows {
		result.Events = append(result.Events, eventFromRow(dbgen.ReadEventsRow(row), state.byEvent[row.ID]))
	}
	return boundedHistoryResult(result, state.tx.Commit(ctx))
}
