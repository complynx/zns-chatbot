package legacyfood

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type authorityDatabaseQuery struct{ err error }

func (q authorityDatabaseQuery) QueryRow(context.Context, string, ...any) pgx.Row {
	return authorityDatabaseRow(q)
}

type authorityDatabaseRow struct{ err error }

func (row authorityDatabaseRow) Scan(...any) error { return row.err }

func TestFoodAuthorityDatabaseOrigins(t *testing.T) {
	t.Parallel()
	for _, failure := range []struct {
		name string
		err  error
		want error
	}{
		{name: "EOF", err: io.EOF, want: core.ErrDatabase},
		{name: "cancellation", err: context.Canceled, want: context.Canceled},
		{name: "deadline", err: context.DeadlineExceeded, want: context.DeadlineExceeded},
	} {
		t.Run(failure.name, func(t *testing.T) {
			t.Parallel()
			query := authorityDatabaseQuery{err: failure.err}
			require.ErrorIs(t, allowed(t.Context(), query, "alice"), failure.want)
			require.ErrorIs(t, adminPermission(t.Context(), query, "alice", "dance", "review", true), failure.want)
		})
	}
	require.Equal(t, forbidden(), allowed(t.Context(), authorityDatabaseQuery{}, "alice"))
	require.Equal(
		t,
		forbidden(),
		adminPermission(t.Context(), authorityDatabaseQuery{err: pgx.ErrNoRows}, "alice", "dance", "review", true),
	)
}
