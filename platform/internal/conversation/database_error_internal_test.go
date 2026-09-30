package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type historyFaultTx struct {
	pgx.Tx

	err        error
	queryPhase string
	summary    json.RawMessage
}

func (tx *historyFaultTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.err
}

func (tx *historyFaultTx) QueryRow(context.Context, string, ...any) pgx.Row {
	return historyFaultRow{err: tx.err, summary: tx.summary}
}

func (tx *historyFaultTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if tx.queryPhase == "query" {
		return nil, tx.err
	}
	return &historyFaultRows{err: tx.err, phase: tx.queryPhase}, nil
}

func (tx *historyFaultTx) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return historyFaultBatch{err: tx.err}
}

type historyFaultRow struct {
	err     error
	summary json.RawMessage
}

func (row historyFaultRow) Scan(dest ...any) error {
	if row.err == nil {
		if data, ok := dest[0].(*json.RawMessage); ok {
			*data = row.summary
		}
	}
	return row.err
}

type historyFaultRows struct {
	pgx.Rows

	err   error
	phase string
}

func (*historyFaultRows) Close()                 {}
func (rows *historyFaultRows) Next() bool        { return rows.phase == "scan" }
func (rows *historyFaultRows) Scan(...any) error { return rows.err }
func (rows *historyFaultRows) Err() error        { return rows.err }

type historyFaultBatch struct {
	pgx.BatchResults

	err error
}

func (batch historyFaultBatch) Close() error { return batch.err }

func TestHistorySQLOriginErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		run  func(context.Context, pgx.Tx) error
	}{
		{"summary lock", func(ctx context.Context, tx pgx.Tx) error { return lockSummary(ctx, tx, "actor") }},
		{"authority lock", func(ctx context.Context, tx pgx.Tx) error {
			return (&authoritySnapshot{tx: tx}).lock(ctx, "actor", nil, authorityScope{})
		}},
		{"read budget", func(ctx context.Context, tx pgx.Tx) error {
			return checkHistoryReadBudget(ctx, tx, "actor", []int64{1}, false, false)
		}},
		{"summary authorities", func(ctx context.Context, tx pgx.Tx) error {
			return (&authoritySnapshot{tx: tx}).loadSummaryAuthorities(ctx, "actor")
		}},
		{"invalidate batch", func(ctx context.Context, tx pgx.Tx) error {
			return invalidateDerived(ctx, tx, "actor", []int64{1}, true)
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			for _, fault := range []error{io.EOF, errors.New("private SQL connection diagnostics"), context.Canceled, context.DeadlineExceeded} {
				err := test.run(t.Context(), &historyFaultTx{err: fault})
				want := core.ErrDatabase
				if errors.Is(fault, context.Canceled) || errors.Is(fault, context.DeadlineExceeded) {
					want = fault
				}
				require.ErrorIs(t, err, want)
				require.Equal(t, want.Error(), err.Error())
			}
		})
	}
}

func TestHistoryGeneratedSQLFailurePhases(t *testing.T) {
	t.Parallel()
	for _, scope := range []authorityScope{{page: Query{Limit: 1}}, {batchBefore: 10}} {
		for _, phase := range []string{"query", "scan", "rows error"} {
			for _, fault := range []error{io.EOF, context.Canceled, context.DeadlineExceeded, pgx.ErrNoRows} {
				_, err := scope.selectedIDs(t.Context(), &historyFaultTx{err: fault, queryPhase: phase}, "actor")
				want := fault
				if errors.Is(fault, io.EOF) {
					want = core.ErrDatabase
				}
				require.ErrorIs(t, err, want, "phase %s", phase)
			}
		}
	}
}

func TestHistoryNonSQLFailuresRemainUnmarked(t *testing.T) {
	t.Parallel()
	state := &authoritySnapshot{tx: &historyFaultTx{err: pgx.ErrNoRows}}
	require.NoError(t, state.loadSummaryAuthorities(t.Context(), "actor"))
	state.tx = &historyFaultTx{summary: json.RawMessage(`{`)}
	err := state.loadSummaryAuthorities(t.Context(), "actor")
	var syntax *json.SyntaxError
	require.ErrorAs(t, err, &syntax)
	require.NotErrorIs(t, err, core.ErrDatabase)
	err = checkHistoryReadBudget(t.Context(), &historyFaultTx{}, "actor", []int64{1}, false, false)
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "read_result_limit", problem.Code)
	require.NotErrorIs(t, err, core.ErrDatabase)
}
