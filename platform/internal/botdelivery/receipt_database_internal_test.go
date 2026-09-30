package botdelivery

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

type receiptSQLFailure struct {
	pgx.Tx

	err error
}

func (tx receiptSQLFailure) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.err
}

func (tx receiptSQLFailure) QueryRow(context.Context, string, ...any) pgx.Row {
	return readDatabaseRow(func(...any) error { return tx.err })
}

func TestReceiptProjectionDatabaseFailureAndBinding(t *testing.T) {
	t.Parallel()
	s := Service{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for _, kind := range []string{"workflow_card", "order_card", "pass_card", "massage_card", familyPassRedaction} {
		i := Intent{State: delivery.Succeeded, MessageID: 1, Receipt: Continuation{Kind: kind}}
		require.ErrorIs(t, s.projectReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, i), core.ErrDatabase)
		require.ErrorIs(t, s.projectReceipt(t.Context(), receiptSQLFailure{err: context.Canceled}, i), core.ErrDatabase)
		require.ErrorIs(t, s.projectReceipt(ctx, receiptSQLFailure{err: context.Canceled}, i), context.Canceled)
	}
	require.ErrorIs(t, s.projectReceipt(t.Context(), receiptSQLFailure{err: io.EOF}, Intent{}), ErrBinding)
}

func TestRenderedTargetDatabaseFailureAndAbsentView(t *testing.T) {
	t.Parallel()
	for _, family := range []string{"workflow", familyPasses, familyMassage, "order"} {
		i := Intent{Reference: Reference{Family: family}}
		require.ErrorIs(t, lockRenderedTarget(t.Context(), receiptSQLFailure{err: io.EOF}, i, 0), core.ErrDatabase)
		require.NoError(t, lockRenderedTarget(t.Context(), receiptSQLFailure{err: pgx.ErrNoRows}, i, 0))
		require.ErrorIs(t, lockRenderedTarget(t.Context(), receiptSQLFailure{err: pgx.ErrNoRows}, i, 1), pgx.ErrNoRows)
	}
}
