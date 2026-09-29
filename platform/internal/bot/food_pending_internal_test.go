package bot

import (
	"context"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func foodPendingDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "food_pending_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	config, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	config.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	return db
}

func TestFoodPendingOrdinalAcrossOrdersAndReplay(t *testing.T) {
	t.Parallel()
	b := &Bot{DB: foodPendingDatabase(t)}
	first := legacyfood.Command{
		EventID: "first",
		OrderID: "order-first",
		Version: 99,
		Name:    foodSubmitProof,
		Kind:    legacyfood.Meals,
	}
	second := legacyfood.Command{
		EventID:    "second",
		OrderID:    "order-second",
		Version:    1,
		Name:       foodSubmitProof,
		Kind:       legacyfood.Activity,
		Generation: 3,
	}
	require.NoError(t, b.storeFoodPending(t.Context(), "alice", 100, first, 2))
	require.NoError(t, b.storeFoodPending(t.Context(), "alice", 100, second, 4))
	for _, replay := range []struct {
		update   int64
		sequence int
		command  legacyfood.Command
	}{
		{100, 2, first},  // Earlier call replay after the second preparation.
		{100, 4, second}, // Exact latest replay.
		{99, 999, first}, // Older update must lose even with a larger ordinal.
	} {
		require.NoError(t, b.storeFoodPending(t.Context(), "alice", replay.update, replay.command, replay.sequence))
		var actual legacyfood.Command
		require.NoError(
			t,
			b.DB.QueryRow(t.Context(), `SELECT command FROM bot.food_pending WHERE owner='alice'`).Scan(&actual),
		)
		assert.Equal(t, second, actual)
	}
	// A later manual update has no script ordinal and still supersedes old hints.
	require.NoError(t, b.storeFoodPending(t.Context(), "alice", 101, first, 0))
	require.NoError(t, b.storeFoodPending(t.Context(), "alice", 100, second, 8))
	var actual legacyfood.Command
	require.NoError(
		t,
		b.DB.QueryRow(t.Context(), `SELECT command FROM bot.food_pending WHERE owner='alice'`).Scan(&actual),
	)
	assert.Equal(t, first, actual)
}
