package readsource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
)

// Stored memory evidence with one derived-proposal ancestor, so memoryEvidence
// runs its second (ancestor chronology) query.
const ancestorEvidence = `[{"causal":{"actor":"alice","generation":0,"private_history":false,` +
	`"authorities":[{"knowledge":{"proposal_id":1,"owner":"alice",` +
	`"kind":"derived_proposal","scope":"","generation":0}}]}}]`

const nonCausalEvidence = `[{"knowledge":{"kind":"review","scope":"a"}}]`

// faultTx scripts only the SQL boundaries owned by readsource; any other call fails the test.
type faultTx struct {
	pgx.Tx

	t       *testing.T
	rows    []faultRow
	query   func() (pgx.Rows, error)
	queries int
}

func (tx *faultTx) QueryRow(context.Context, string, ...any) pgx.Row {
	require.NotEmpty(tx.t, tx.rows, "unexpected QueryRow")
	row := tx.rows[0]
	tx.rows = tx.rows[1:]
	return row
}

func (tx *faultTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	require.NotNil(tx.t, tx.query, "unexpected Query")
	tx.queries++
	return tx.query()
}

type faultRow func(dest ...any) error

func (row faultRow) Scan(dest ...any) error { return row(dest...) }

type faultRows struct {
	pgx.Rows

	next    int
	scanErr error
	err     error
}

func (rows *faultRows) Next() bool {
	if rows.next == 0 {
		return false
	}
	rows.next--
	return true
}
func (rows *faultRows) Scan(...any) error { return rows.scanErr }
func (rows *faultRows) Err() error        { return rows.err }
func (*faultRows) Close()                 {}

func scanInto[T any](dest any, value T) error {
	target, ok := dest.(*T)
	if !ok {
		return fmt.Errorf("unexpected scan destination %T", dest)
	}
	*target = value
	return nil
}

func failedRow(err error) faultRow { return func(...any) error { return err } }

func memoryRow(raw string) faultRow {
	return func(dest ...any) error {
		return errors.Join(scanInto(dest[0], []byte(raw)), scanInto(dest[1], time.Unix(100, 0).UTC()))
	}
}

func proposalRow(raw string) faultRow {
	return func(dest ...any) error {
		return errors.Join(scanInto(dest[0], []byte(raw)), scanInto(dest[1], false))
	}
}

func earlierRow(earlier bool) faultRow {
	return func(dest ...any) error { return scanInto(dest[0], earlier) }
}

func rowsResult(rows pgx.Rows, err error) func() (pgx.Rows, error) {
	return func() (pgx.Rows, error) { return rows, err }
}

// connectionEOF stands in for a dropped PostgreSQL connection carrying a network address.
func connectionEOF() error { return fmt.Errorf("read tcp 10.0.0.1:5432: %w", io.ErrUnexpectedEOF) }

func memoryRef() Authority {
	return Authority{Knowledge: knowledgeauthority.ReadAuthority{
		Kind:       knowledgeauthority.DerivedMemory,
		Namespace:  "shared",
		Topic:      "topic",
		Key:        "key",
		SourceKind: "fact",
		Version:    2,
	}}
}

func proposalRef(id int64) Authority {
	return Authority{Knowledge: knowledgeauthority.ReadAuthority{
		Kind:       knowledgeauthority.DerivedProposal,
		Owner:      "alice",
		ProposalID: id,
	}}
}

func expand(ref Authority) func(context.Context, pgx.Tx) error {
	return func(ctx context.Context, tx pgx.Tx) error {
		_, err := ExpandProposalSources(ctx, tx, []Authority{ref})
		return err
	}
}

func lockReviewActors(ctx context.Context, tx pgx.Tx) error {
	review := Authority{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "a"}}
	return LockActors(ctx, tx, []string{"alice"}, []Authority{review})
}

type boundaryCase struct {
	rows  []faultRow
	query func() (pgx.Rows, error)
	run   func(context.Context, pgx.Tx) error
}

func runBoundary(t *testing.T, tc boundaryCase) error {
	t.Helper()
	tx := &faultTx{t: t, rows: tc.rows, query: tc.query}
	err := tc.run(t.Context(), tx)
	require.Empty(t, tx.rows, "every scripted SQL boundary must be reached")
	if tc.query != nil {
		require.Equal(t, 1, tx.queries)
	}
	return err
}

func TestReadsourceSQLBoundariesSanitizeConnectionEOF(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]boundaryCase{
		"memory evidence": {rows: []faultRow{failedRow(connectionEOF())}, run: expand(memoryRef())},
		"memory ancestor": {
			rows: []faultRow{memoryRow(ancestorEvidence), failedRow(connectionEOF())},
			run:  expand(memoryRef()),
		},
		"proposal evidence": {rows: []faultRow{failedRow(connectionEOF())}, run: expand(proposalRef(5))},
		"actor query":       {query: rowsResult(nil, connectionEOF()), run: lockReviewActors},
		"actor scan": {
			query: rowsResult(&faultRows{next: 1, scanErr: connectionEOF()}, nil),
			run:   lockReviewActors,
		},
		"actor rows": {query: rowsResult(&faultRows{err: connectionEOF()}, nil), run: lockReviewActors},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := runBoundary(t, tc)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.NotErrorIs(t, err, io.ErrUnexpectedEOF)
			require.NotContains(t, err.Error(), "10.0.0.1")
		})
	}
}

func TestReadsourceSQLBoundariesPreserveAbsence(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		ref  Authority
		rows []faultRow
		size int
	}{
		"memory evidence":   {memoryRef(), []faultRow{failedRow(pgx.ErrNoRows)}, 1},
		"proposal evidence": {proposalRef(5), []faultRow{failedRow(pgx.ErrNoRows)}, 1},
		"memory ancestor":   {memoryRef(), []faultRow{memoryRow(ancestorEvidence), failedRow(pgx.ErrNoRows)}, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tx := &faultTx{t: t, rows: tc.rows}
			got, err := ExpandProposalSources(t.Context(), tx, []Authority{tc.ref})
			require.NoError(t, err, "a missing source or ancestor is not a loader error")
			require.Empty(t, tx.rows)
			require.Len(t, got, tc.size)
			require.Contains(t, got, tc.ref)
		})
	}
	t.Run("actor rows", func(t *testing.T) {
		t.Parallel()
		err := runBoundary(t, boundaryCase{query: rowsResult(&faultRows{next: 1}, nil), run: lockReviewActors})
		require.NoError(t, err)
	})
}

func TestReadsourceSQLBoundariesPreserveCancellation(t *testing.T) {
	t.Parallel()
	canceled := fmt.Errorf("query: %w", context.Canceled)
	deadline := fmt.Errorf("query: %w", context.DeadlineExceeded)
	for name, tc := range map[string]struct {
		boundaryCase

		want error
	}{
		"memory evidence": {
			boundaryCase{rows: []faultRow{failedRow(canceled)}, run: expand(memoryRef())},
			context.Canceled,
		},
		"memory ancestor": {
			boundaryCase{rows: []faultRow{memoryRow(ancestorEvidence), failedRow(deadline)}, run: expand(memoryRef())},
			context.DeadlineExceeded,
		},
		"proposal evidence": {
			boundaryCase{rows: []faultRow{failedRow(deadline)}, run: expand(proposalRef(5))},
			context.DeadlineExceeded,
		},
		"actor query": {
			boundaryCase{query: rowsResult(nil, deadline), run: lockReviewActors},
			context.DeadlineExceeded,
		},
		"actor rows": {
			boundaryCase{query: rowsResult(&faultRows{err: canceled}, nil), run: lockReviewActors},
			context.Canceled,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			if name == "actor query" || name == "actor rows" {
				ctx = actorParentContext(t, tc.want)
			}
			tx := &faultTx{t: t, rows: tc.rows, query: tc.query}
			err := tc.run(ctx, tx)
			require.Empty(t, tx.rows)
			if tc.query != nil {
				require.Equal(t, 1, tx.queries)
			}
			require.ErrorIs(t, err, tc.want)
			require.NotErrorIs(t, err, core.ErrDatabase)
		})
	}
}

func TestReadsourceStoredEvidenceErrorsStayDomainErrors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		boundaryCase

		limit bool
	}{
		"memory json":   {boundaryCase{rows: []faultRow{memoryRow("{")}, run: expand(memoryRef())}, false},
		"proposal json": {boundaryCase{rows: []faultRow{proposalRow("{")}, run: expand(proposalRef(5))}, false},
		"memory null":   {boundaryCase{rows: []faultRow{memoryRow("null")}, run: expand(memoryRef())}, true},
		"memory non-causal": {
			boundaryCase{rows: []faultRow{memoryRow(nonCausalEvidence)}, run: expand(memoryRef())},
			true,
		},
		"proposal non-causal": {
			boundaryCase{rows: []faultRow{proposalRow(nonCausalEvidence)}, run: expand(proposalRef(5))},
			true,
		},
		"proposal forward ref": {
			boundaryCase{rows: []faultRow{proposalRow(ancestorEvidence)}, run: expand(proposalRef(1))},
			true,
		},
		"memory later ancestor": {
			boundaryCase{rows: []faultRow{memoryRow(ancestorEvidence), earlierRow(false)}, run: expand(memoryRef())},
			true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := runBoundary(t, tc.boundaryCase)
			require.Error(t, err)
			require.NotErrorIs(t, err, core.ErrDatabase)
			if tc.limit {
				require.ErrorIs(t, err, ErrLimit)
				return
			}
			_, ok := errors.AsType[*json.SyntaxError](err)
			require.True(t, ok, "stored JSON corruption is not a database failure")
		})
	}
}

func TestReadsourceInvalidInputSkipsDatabase(t *testing.T) {
	t.Parallel()
	invalid := []Authority{{}}

	err := lockActors(t.Context(), &faultTx{t: t}, nil, invalid, true)
	require.EqualError(t, err, "invalid source authority")
	require.NotErrorIs(t, err, core.ErrDatabase)

	_, err = lockFlatValidity(t.Context(), &faultTx{t: t}, "alice", invalid)
	problem, ok := errors.AsType[*core.ProblemError](err)
	require.True(t, ok)
	require.Equal(t, http.StatusBadRequest, problem.Status)
	require.Equal(t, "invalid_history", problem.Code)
	require.NotErrorIs(t, err, core.ErrDatabase)

	_, err = ExpandProposalSources(t.Context(), &faultTx{t: t}, invalid)
	require.ErrorIs(t, err, ErrLimit)
	require.NotErrorIs(t, err, core.ErrDatabase)
}
