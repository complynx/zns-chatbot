package destination_test

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
)

func TestDestinationDatabaseProvenance(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		err      error
		database bool
	}{
		{name: "SQL EOF", err: core.DatabaseOperationError(io.EOF), database: true},
		{name: "SQL and cancellation", err: errors.Join(core.ErrDatabase, context.Canceled), database: true},
		{name: "driver failure", err: &pgconn.PgError{Code: "XX000", Message: "private SQL detail"}, database: true},
		{name: "provider EOF", err: io.EOF},
		{name: "provider cancellation", err: context.Canceled},
		{name: "provider deadline", err: context.DeadlineExceeded},
		{name: "domain unavailable", err: destination.ErrUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			resolver := resolverFunc(func(_ context.Context, alias string) (int64, error) {
				if alias == "@healthy" {
					return 123, nil
				}
				return 0, test.err
			})
			_, err := destination.Resolve(t.Context(), resolver, []string{"@failed"})
			require.ErrorIs(t, err, destination.ErrUnavailable)
			require.Equal(t, test.database, core.IsDatabaseFailure(err))
			require.Equal(t, destination.ErrUnavailable.Error(), err.Error())
			bindings := &destination.Bindings{}
			err = bindings.Refresh(t.Context(), resolver, []string{"@failed", "@healthy"}, time.Minute)
			require.ErrorIs(t, err, destination.ErrUnavailable)
			require.Equal(t, test.database, core.IsDatabaseFailure(err))
			require.Equal(t, destination.ErrUnavailable.Error(), err.Error())
			chat, err := bindings.Lookup("@healthy")
			require.NoError(t, err)
			require.Equal(t, "123", chat)
		})
	}
}
