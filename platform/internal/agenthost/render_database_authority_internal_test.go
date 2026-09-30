package agenthost

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestRenderDatabaseMissingReplyAuthority(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{io.EOF, context.Canceled, context.DeadlineExceeded} {
		config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
		require.NoError(t, err)
		config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return failure }
		db, err := pgxpool.NewWithConfig(t.Context(), config)
		require.NoError(t, err)
		t.Cleanup(db.Close)
		policy := PlanAuthorization{DB: db}
		err = policy.validateMissingReply(t.Context(), "alice", 1)
		require.ErrorIs(t, err, core.ErrDatabase)
		err = policy.ValidateHistoryInteractions(t.Context(), "alice", 1, 1)
		require.ErrorIs(t, err, core.ErrDatabase)
	}
}
