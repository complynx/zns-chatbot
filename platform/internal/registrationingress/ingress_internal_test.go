package registrationingress

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type failingIngressTx struct {
	pgx.Tx

	calls  int
	failAt int
}

func (tx *failingIngressTx) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	tx.calls++
	if tx.calls == tx.failAt {
		return pgconn.CommandTag{}, io.EOF
	}
	return pgconn.NewCommandTag("SELECT 1"), nil
}

func TestSaveClassifiedTelegramMarksSQLTransportFailures(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{1, 2} {
		tx := &failingIngressTx{failAt: failAt}
		err := SaveClassifiedTelegram(t.Context(), tx, Reference{BotID: 7, UpdateID: 1}, 101, nil)
		require.ErrorIs(t, err, core.ErrDatabase)
		require.NotErrorIs(t, err, io.EOF, "driver diagnostics must not escape")
		require.Equal(t, failAt, tx.calls)
	}
	tx := &failingIngressTx{failAt: 1}
	err := SaveClassifiedTelegram(t.Context(), tx, Reference{}, 101, nil)
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err), "invalid input has no SQL provenance")
	require.Zero(t, tx.calls)
}
