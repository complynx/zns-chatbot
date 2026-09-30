package derivedmutation

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

type nativeSourceTx struct {
	pgx.Tx

	err error
}

func (tx nativeSourceTx) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nativeSourceRows{}, nil
}
func (tx nativeSourceTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	if strings.Contains(sql, "SELECT EXISTS(SELECT 1 FROM core.users") {
		return nativeSourceAllowedRow{}
	}
	return nativeDatabaseRow{err: tx.err}
}
func (tx nativeSourceTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

type nativeSourceRows struct{ pgx.Rows }

func (nativeSourceRows) Next() bool { return false }
func (nativeSourceRows) Err() error { return nil }
func (nativeSourceRows) Close()     {}

type nativeSourceAllowedRow struct{}

func (nativeSourceAllowedRow) Scan(values ...any) error {
	*values[0].(*bool) = true
	return nil
}

func TestNativeDatabaseSourceGraph(t *testing.T) {
	t.Parallel()
	for _, source := range []struct {
		name      string
		authority readsource.Authority
	}{
		{name: "knowledge", authority: readsource.Authority{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "dance"}}},
		{name: "food", authority: readsource.Authority{Food: legacyfood.ReadAuthority{Event: "dance", Scope: "review"}}},
		{name: "practitioner", authority: readsource.Authority{Practitioner: massage.ReadAuthority{Event: "dance", Owner: "alice"}}},
	} {
		t.Run(source.name, func(t *testing.T) {
			t.Parallel()
			for _, failure := range []struct {
				name string
				err  error
				want error
			}{
				{name: "SQL EOF", err: io.EOF, want: core.ErrDatabase},
				{name: "cancellation", err: context.Canceled, want: context.Canceled},
				{name: "deadline", err: context.DeadlineExceeded, want: context.DeadlineExceeded},
			} {
				t.Run(failure.name, func(t *testing.T) {
					t.Parallel()
					err := lockSource(t.Context(), nativeSourceTx{err: failure.err}, "alice",
						readsource.Derivation{Authorities: []readsource.Authority{source.authority}})
					require.ErrorIs(t, err, failure.want)
					require.Equal(t, errors.Is(failure.want, core.ErrDatabase), core.IsDatabaseFailure(err))
				})
			}
			err := lockSource(t.Context(), nativeSourceTx{err: pgx.ErrNoRows}, "alice",
				readsource.Derivation{Authorities: []readsource.Authority{source.authority}})
			var problem *core.ProblemError
			require.ErrorAs(t, err, &problem)
			require.Equal(t, "source_stale", problem.Code)
			require.False(t, core.IsDatabaseFailure(err))
		})
	}
}
