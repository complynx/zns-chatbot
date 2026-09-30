package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestAccountLanguageDatabaseFailureRollsBackAndRecovers(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"UPDATE core.users SET language=", "INSERT INTO core.language_operations", "commit"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			healthy := account.Service{DB: f.db}
			initial, err := healthy.Preferences(t.Context(), "alice")
			require.NoError(t, err)
			target := "ru"
			if initial.Language == target {
				target = "en"
			}
			fault := &renderReadFailure{query: stage}
			config := f.db.Config()
			config.ConnConfig.Tracer = fault
			broken, err := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, err)
			t.Cleanup(broken.Close)
			service := account.Service{DB: broken}
			const key account.LanguageOperationKey = "language-sql-recovery"
			_, err = service.SetLanguageWithOperation(t.Context(), "alice", target, false, key)
			require.True(t, fault.fired)
			require.NoError(t, fault.closeErr)
			require.ErrorIs(t, err, core.ErrDatabase)
			require.EqualError(t, err, core.ErrDatabase.Error())
			current, err := healthy.Preferences(t.Context(), "alice")
			require.NoError(t, err)
			require.Equal(t, initial, current)
			var receipts int
			require.NoError(t, f.db.QueryRow(
				t.Context(),
				`SELECT count(*) FROM core.language_operations WHERE owner='alice' AND operation_key=$1`,
				key,
			).Scan(&receipts))
			require.Zero(t, receipts)
			current, err = healthy.SetLanguageWithOperation(t.Context(), "alice", target, false, key)
			require.NoError(t, err)
			require.Equal(t, target, current.Language)
			replay, err := healthy.SetLanguageWithOperation(t.Context(), "alice", target, false, key)
			require.NoError(t, err)
			require.Equal(t, current, replay)
			require.NoError(t, f.db.QueryRow(
				t.Context(),
				`SELECT count(*) FROM core.language_operations WHERE owner='alice' AND operation_key=$1`,
				key,
			).Scan(&receipts))
			require.Equal(t, 1, receipts)
		})
	}
}
