package legacyfood

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const reminderTestTimeout = 5 * time.Second

// droppingServer completes the PostgreSQL startup and `completed` simple
// statements, then drops the established connection on the next statement,
// as a server restart or network cut would. The client then observes a
// generic transport error, neither a PgError nor a ConnectError.
func droppingServer(conn net.Conn, completed int) {
	defer conn.Close()
	backend := pgproto3.NewBackend(conn, conn)
	if _, err := backend.ReceiveStartupMessage(); err != nil {
		return
	}
	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if backend.Flush() != nil {
		return
	}
	for range completed {
		if _, err := backend.Receive(); err != nil {
			return
		}
		backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("BEGIN")})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'T'})
		if backend.Flush() != nil {
			return
		}
	}
	_, _ = backend.Receive()
}

func droppingService(t *testing.T, completed int) Service {
	t.Helper()
	config, err := pgxpool.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	config.ConnConfig.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		client, server := net.Pipe()
		go droppingServer(server, completed)
		return client, nil
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return Service{DB: pool}
}

// An established connection dropped at Begin or at the first reminder query
// returns a raw EOF-class error from pgx; QueueReminders must still report
// the safe database marker so the maintenance supervisor can restart.
func TestQueueRemindersClassifiesDroppedConnectionAtSQLOrigins(t *testing.T) {
	t.Parallel()
	for name, completed := range map[string]int{"begin": 0, "order reminder query": 1} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), reminderTestTimeout)
			defer cancel()
			err := droppingService(t, completed).QueueReminders(ctx)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.EqualError(t, err, core.ErrDatabase.Error())
			assert.NotContains(t, err.Error(), "private-")
		})
	}
}

// failingTx fails every statement with err; other Tx methods are unused.
type failingTx struct {
	pgx.Tx

	err error
}

func (f failingTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, f.err
}

func (f failingTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, f.err }

func (f failingTx) QueryRow(context.Context, string, ...any) pgx.Row { return failingRow{err: f.err} }

type failingRow struct{ err error }

func (r failingRow) Scan(...any) error { return r.err }

func TestCollectNotificationRegistrationPauseKeepsSQLProvenance(t *testing.T) {
	t.Parallel()
	transport := fmt.Errorf("private-transport-canary: %w", io.ErrUnexpectedEOF)
	require.False(t, core.IsDatabaseFailure(transport), "a raw transport error alone is not classified")
	var pending []delivery.Registration

	err := collectNotificationRegistration(t.Context(), failingTx{err: transport}, 1, 7, 0, &pending)
	require.ErrorIs(t, err, core.ErrDatabase)
	assert.NotContains(t, err.Error(), "private-transport-canary")

	err = collectNotificationRegistration(t.Context(), failingTx{err: context.Canceled}, 1, 7, 0, &pending)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err), "cancellation stays non-fatal")
	assert.Empty(t, pending)

	err = collectNotificationRegistration(t.Context(), failingTx{err: transport}, 1, 7, 42, &pending)
	require.NoError(t, err, "a routable row is only collected; it performs no SQL here")
	require.Len(t, pending, 1)
	assert.Equal(t, "42", pending[0].Destination.Chat)
}
