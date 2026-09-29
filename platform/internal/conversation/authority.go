package conversation

import (
	"context"
	"net/http"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type authoritySnapshot struct {
	tx      pgx.Tx
	byEvent map[int64][]readsource.Authority
	summary []readsource.Authority
	allowed bool
	ids     []int64
}

type authorityScope struct {
	textOnly    bool
	page        Query
	ids         []int64
	batchBefore int64
	summary     bool
}

// Begin a coherent history read. The advisory lock prevents a concurrent append
// from adding provenance between discovery and event locks. Domain event locks
// precede the summary lock, matching registration mutations and derived writes.
func (s Service) authoritySnapshot(
	ctx context.Context,
	actor string,
	extra []readsource.Authority,
	scope authorityScope,
) (*authoritySnapshot, error) {
	if !readsource.Valid(extra) {
		return nil, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidHistory}
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	state := &authoritySnapshot{tx: tx, byEvent: map[int64][]readsource.Authority{}, allowed: true}
	if err = state.lock(ctx, actor, extra, scope); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	return state, nil
}

func (s *authoritySnapshot) lock(
	ctx context.Context,
	actor string,
	extra []readsource.Authority,
	scope authorityScope,
) error {
	if _, err := s.tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('conversation-authority:' || $1,0))`,
		actor,
	); err != nil {
		return err
	}
	ids, err := scope.selectedIDs(ctx, s.tx, actor)
	if err != nil {
		return err
	}
	s.ids = ids
	if err = checkHistoryReadBudget(ctx, s.tx, actor, ids, scope.summary, scope.textOnly); err != nil {
		return err
	}
	rows, err := dbgen.New(s.tx).ReadAuthorities(ctx, dbgen.ReadAuthoritiesParams{Owner: actor, Column2: ids})
	if err != nil {
		return err
	}
	all, err := s.loadEventAuthorities(rows, extra)
	if err != nil {
		return err
	}
	if scope.summary {
		if err = s.loadSummaryAuthorities(ctx, actor); err != nil {
			return err
		}
		all = append(all, s.summary...)
	}
	valid, err := readsource.Lock(ctx, s.tx, actor, all)
	if err != nil {
		return err
	}
	s.allowed = !slices.Contains(valid[:len(extra)], false)
	return s.invalidateStale(ctx, actor, scope.summary, rows, valid, len(extra))
}

func (s *authoritySnapshot) invalidateStale(
	ctx context.Context,
	actor string,
	checkSummary bool,
	rows []dbgen.CoreConversationReadAuthority,
	valid []bool,
	index int,
) error {
	if err := lockSummary(ctx, s.tx, actor); err != nil {
		return err
	}
	var generation int64
	if err := s.tx.QueryRow(ctx, `SELECT COALESCE((SELECT generation FROM core.conversation_history_generations WHERE owner=$1),0)`, actor).
		Scan(&generation); err != nil {
		return err
	}
	stale, err := dbgen.New(s.tx).UnprovenRuntimeIDs(ctx, dbgen.UnprovenRuntimeIDsParams{Owner: actor, Column2: s.ids})
	if err != nil {
		return err
	}
	var unknownSummary bool
	if checkSummary {
		unknownSummary, err = dbgen.New(s.tx).HasUnprovenRuntimeSummary(ctx, actor)
		if err != nil {
			return err
		}
	}
	advanceGeneration := len(stale) > 0 || unknownSummary
	for _, row := range rows {
		size := len(s.byEvent[row.EventID])
		if row.HistoryGeneration != generation || slices.Contains(valid[index:index+size], false) {
			// An older row was already invalidated by the existing epoch. Its
			// lazy body cleanup must not invalidate work admitted afterward.
			advanceGeneration = advanceGeneration || row.HistoryGeneration >= generation
			stale = append(stale, row.EventID)
			delete(s.byEvent, row.EventID)
		}
		index += size
	}
	advanceGeneration = advanceGeneration || slices.Contains(valid[index:], false)
	if advanceGeneration {
		// A new epoch also retires the other selected derived rows. Do this
		// before returning bodies under the resulting page generation.
		for id := range s.byEvent {
			stale = append(stale, id)
			delete(s.byEvent, id)
		}
	}
	if len(stale) == 0 && !advanceGeneration {
		return nil
	}
	if err = lockSummary(ctx, s.tx, actor); err != nil {
		return err
	}
	s.summary = nil
	return invalidateDerived(ctx, s.tx, actor, stale, advanceGeneration)
}

func (s authorityScope) selectedIDs(ctx context.Context, tx pgx.Tx, actor string) ([]int64, error) {
	queries := dbgen.New(tx)
	if s.page.Limit > 0 {
		return queries.AuthorityPageIDs(
			ctx,
			dbgen.AuthorityPageIDsParams{
				Owner:     actor,
				BeforeID:  s.page.Before,
				AfterID:   s.page.After,
				PageLimit: int64(s.page.Limit),
			},
		)
	}
	if s.batchBefore > 0 {
		return queries.AuthorityBatchIDs(
			ctx,
			dbgen.AuthorityBatchIDsParams{Owner: actor, ID: s.batchBefore, Limit: MaxPage},
		)
	}
	return s.ids, nil
}

func lockSummary(ctx context.Context, tx pgx.Tx, actor string) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.conversation_summaries(owner) VALUES($1) ON CONFLICT DO NOTHING`,
		actor,
	); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `SELECT version FROM core.conversation_summaries WHERE owner=$1 FOR UPDATE`, actor)
	return err
}

func invalidateDerived(ctx context.Context, tx pgx.Tx, actor string, ids []int64, advanceGeneration bool) error {
	batch := new(pgx.Batch)
	batch.Queue(
		`UPDATE core.conversation_events SET text='',details='{}',omitted=true,omission_reason='authority_revoked' WHERE owner=$1 AND id=ANY($2)`,
		actor,
		ids,
	)
	batch.Queue(`DELETE FROM core.conversation_message_bodies WHERE event_id=ANY($1)`, ids)
	batch.Queue(`UPDATE core.conversation_events SET summarized_version=0 WHERE owner=$1`, actor)
	batch.Queue(
		`UPDATE core.conversation_summaries SET version=version+1,through_id=0,text='',read_authorities='[]' WHERE owner=$1`,
		actor,
	)
	if advanceGeneration {
		batch.Queue(
			`INSERT INTO core.conversation_history_generations(owner,generation) VALUES($1,1) ON CONFLICT(owner) DO UPDATE SET generation=core.conversation_history_generations.generation+1`,
			actor,
		)
	}
	return tx.SendBatch(ctx, batch).Close()
}

func (s *authoritySnapshot) summaryAuthorities(ids []int64) ([]readsource.Authority, error) {
	result := append([]readsource.Authority(nil), s.summary...)
	for id, authorities := range s.byEvent {
		if !slices.Contains(ids, id) {
			continue
		}
		for _, authority := range authorities {
			if !slices.ContainsFunc(
				result,
				func(existing readsource.Authority) bool { return readsource.Equal(existing, authority) },
			) {
				result = append(result, authority)
			}
		}
	}
	if !readsource.Valid(result) {
		return nil, &core.ProblemError{Status: http.StatusConflict, Code: "history_authority_limit"}
	}
	return result, nil
}

func staleHistory() error {
	return &core.ProblemError{Status: http.StatusConflict, Code: historyStale}
}

// CheckReadAuthorities validates trusted host evidence at an exposure boundary.
func (s Service) CheckReadAuthorities(
	ctx context.Context,
	actor string,
	authorities []readsource.Authority,
) error {
	state, err := s.authoritySnapshot(ctx, actor, authorities, authorityScope{})
	if err != nil {
		return err
	}
	defer func() { _ = state.tx.Rollback(ctx) }()
	if err = state.tx.Commit(ctx); err != nil {
		return err
	}
	if !state.allowed {
		return staleHistory()
	}
	return nil
}
