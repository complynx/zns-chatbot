package bot

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestInboxSQLLocalContextFailureStopsWithoutPoisonBudget(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.DeadlineExceeded, context.Canceled} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			db, attempts := inboxSQLContextFailurePool(t, failure)
			_, err := (&Bot{DB: db}).bindBudget(t.Context(), "alice", 1)
			require.Error(t, err)
			require.Positive(t, attempts.Load(), "the actual pool connection boundary was reached")
			require.NoError(t, t.Context().Err(), "the request parent is still live")
			require.ErrorIs(t, err, core.ErrDatabase)
			assert.True(t, core.IsDatabaseFailure(err))
			assert.NotContains(t, err.Error(), "private connection diagnostic")
			outcome, _ := classifyInboxResult(t.Context(), err, 0)
			assert.Equal(t, inboxStop, outcome, "a SQL-local context failure must not consume poison budget")
		})
	}
}

func TestInboxProviderDeadlineRemainsOrdinaryFailure(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("provider request: %w", context.DeadlineExceeded)
	require.False(t, core.IsDatabaseFailure(err))
	outcome, _ := classifyInboxResult(t.Context(), err, 0)
	assert.Equal(t, inboxFail, outcome)
}

func TestInboxSQLParentCancellationRemainsCancellation(t *testing.T) {
	t.Parallel()
	db, _ := inboxSQLContextFailurePool(t, context.Canceled)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := (&Bot{DB: db}).bindBudget(ctx, "alice", 1)
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, core.IsDatabaseFailure(err))
	outcome, _ := classifyInboxResult(ctx, err, 0)
	assert.Equal(t, inboxStop, outcome)
}

func TestInboxMediaSQLLocalDeadlineRetainsOrigin(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		call func(context.Context, *Bot) error
	}{
		{"inbox claim", func(ctx context.Context, b *Bot) error {
			_, _, err := b.claimInboxRow(ctx)
			return err
		}},
		{"media intake", func(ctx context.Context, b *Bot) error {
			_, err := b.loadMediaIntake(ctx, "alice", "media")
			return err
		}},
		{"media outcome", func(ctx context.Context, b *Bot) error {
			_, err := b.loadMediaOutcome(ctx, "alice", "media")
			return err
		}},
		{"media upload", func(ctx context.Context, b *Bot) error {
			_, _, err := b.loadMediaUploadState(ctx, "alice", "media")
			return err
		}},
		{"media hint", func(ctx context.Context, b *Bot) error {
			_, err := b.visibleMediaHint(ctx, "alice", "media", "receipt")
			return err
		}},
		{"media history", func(ctx context.Context, b *Bot) error {
			_, err := b.mediaRecent(ctx, "alice")
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db, attempts := inboxSQLContextFailurePool(t, context.DeadlineExceeded)
			err := test.call(t.Context(), &Bot{DB: db})
			require.Positive(t, attempts.Load())
			require.NoError(t, t.Context().Err())
			requireSafeDatabaseFailure(t, err)
			outcome, _ := classifyInboxResult(t.Context(), err, 0)
			assert.Equal(t, inboxStop, outcome)
		})
	}
}

func inboxSQLContextFailurePool(t *testing.T, failure error) (*pgxpool.Pool, *atomic.Int32) {
	t.Helper()
	config, err := pgxpool.ParseConfig("postgres://unused@127.0.0.1/unused?sslmode=disable")
	require.NoError(t, err)
	attempts := new(atomic.Int32)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error {
		attempts.Add(1)
		return fmt.Errorf("private connection diagnostic: %w", failure)
	}
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	return db, attempts
}
