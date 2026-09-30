package interaction_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

func TestRenderDatabasePrivacyOrigins(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	s := interaction.Store{DB: db}
	_, err = s.ReplyOrigin(t.Context(), "alice", 1)
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = s.LatestNotice(t.Context(), "alice")
	require.ErrorIs(t, err, core.ErrDatabase)
	_, err = s.Load(t.Context(), "alice", 1)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.ErrorIs(t, s.MarkTerminal(t.Context(), "alice", 1, 1, interaction.SourceRevoked), core.ErrDatabase)
	_, err = s.Load(t.Context(), "", 1)
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
	err = s.MarkTerminal(t.Context(), "alice", 1, -1, interaction.SourceRevoked)
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
}

type renderTerminalFault struct {
	prefix   string
	fired    bool
	closeErr error
}

func (f *renderTerminalFault) TraceQueryStart(
	ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if !f.fired && strings.HasPrefix(data.SQL, f.prefix) {
		f.fired = true
		f.closeErr = conn.Close(ctx)
	}
	return ctx
}

func (*renderTerminalFault) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestRenderDatabasePrivacyTerminalRollback(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{
		"-- name: MarkTerminal", "-- name: ClearDerivedReplies", "-- name: InsertTerminalReply", "commit",
	} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			db := turnDatabase(t)
			s := interaction.Store{DB: db}
			require.NoError(t, s.MarkTerminal(t.Context(), "alice", 1, 0, interaction.SourceRevoked))
			fault := &renderTerminalFault{prefix: stage}
			config := db.Config()
			config.ConnConfig.Tracer = fault
			broken, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(broken.Close)
			err = (interaction.Store{DB: broken}).MarkTerminal(t.Context(), "alice", 1, 1, interaction.SourceRevoked)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.True(t, fault.fired)
			require.NoError(t, fault.closeErr)
			unchanged, err := s.Load(t.Context(), "alice", 1)
			require.NoError(t, err)
			require.Zero(t, unchanged.HistoryGeneration)
			require.NoError(t, s.MarkTerminal(t.Context(), "alice", 1, 1, interaction.SourceRevoked))
			finished, err := s.Load(t.Context(), "alice", 1)
			require.NoError(t, err)
			require.EqualValues(t, 1, finished.HistoryGeneration)
		})
	}
}
