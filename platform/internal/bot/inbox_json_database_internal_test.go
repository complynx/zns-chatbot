package bot

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestInboxIncompatiblePayloadIsQuarantinedWithoutBlocking(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	b := &Bot{DB: db}
	const incompatible = `{"update_id":"private-incompatible"}`
	insertInboxRetryRow(t, db, 41, incompatible)
	insertInboxRetryRow(t, db, 42, inboxRetryPayload(t, 42, inboxRetryChatA))
	var calls []int64
	require.NoError(t, b.drainInboxWith(t.Context(), func(_ context.Context, update telegram.Update) error {
		calls = append(calls, update.ID)
		return nil
	}), "a readable incompatible row is not a database failure and does not stop the drain")
	require.Equal(t, []int64{42}, calls, "the undecodable row never reaches the handler")
	state, found := readInboxRetryState(t, db, 41)
	require.True(t, found, "an undecodable update is retained, not acknowledged")
	require.Equal(t, "quarantined", state.State)
	require.Equal(t, inboxFailureMalformed, state.Failure, "only a fixed diagnostic is stored")
	require.Zero(t, state.Failures, "no handler failure was recorded")
	require.JSONEq(t, incompatible, state.Payload, "the original payload is retained")
	require.NoError(t, b.drainInboxWith(t.Context(), func(context.Context, telegram.Update) error {
		require.Fail(t, "a quarantined row is never dispatched automatically")
		return nil
	}), "an inbox with only quarantined rows remains a normal control")
}

func TestInboxSQLTransportFailureIsDatabaseFailure(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	err = (&Bot{DB: db}).drainInbox(t.Context())
	require.Equal(t, core.ErrDatabase, err)
}
