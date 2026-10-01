package passbooking

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type registrationTestClock struct {
	now time.Time
	err error
}

func (c *registrationTestClock) Now(context.Context) (time.Time, error) { return c.now, c.err }

type clockRow struct {
	now time.Time
	err error
}

func (r clockRow) Scan(dest ...any) error {
	if r.err == nil {
		*dest[0].(*time.Time) = r.now
	}
	return r.err
}

type clockTx struct {
	pgx.Tx

	row     clockRow
	queries []string
}

func (tx *clockTx) QueryRow(_ context.Context, query string, _ ...any) pgx.Row {
	tx.queries = append(tx.queries, query)
	return tx.row
}

func TestRegistrationTimeKeepsDefaultSQLAuthority(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	tx := &clockTx{row: clockRow{now: now}}
	observed, err := registrationTime(t.Context(), tx, nil)
	require.NoError(t, err)
	require.Equal(t, now, observed)
	require.Equal(t, []string{"SELECT clock_timestamp()"}, tx.queries)
	tx.row.err = io.EOF
	_, err = registrationTime(t.Context(), tx, nil)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotErrorIs(t, err, io.EOF)
}

func TestRegistrationTimeObservesBindingAtEachCall(t *testing.T) {
	t.Parallel()
	tx := &clockTx{}
	clock := &registrationTestClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	first, err := registrationTime(t.Context(), tx, clock)
	require.NoError(t, err)
	clock.now = clock.now.Add(time.Hour)
	second, err := registrationTime(t.Context(), tx, clock)
	require.NoError(t, err)
	require.Equal(t, time.Hour, second.Sub(first))
	require.Empty(t, tx.queries)
	clock.err = io.EOF
	_, err = registrationTime(t.Context(), tx, clock)
	require.ErrorIs(t, err, io.EOF)
	require.False(t, core.IsDatabaseFailure(err))
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = registrationTime(canceled, tx, clock)
	require.ErrorIs(t, err, context.Canceled)
}
