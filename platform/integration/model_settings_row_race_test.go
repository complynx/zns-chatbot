package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

func TestModelSettingsRejectConcurrentRowWriter(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"update", "insert", "delete"} {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			db, _ := bookingFixture(t)
			service := modelsettings.Service{DB: db}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var version int64
			if mutation != "insert" {
				state, err := service.Set(ctx, "bob", "alice", modelsettings.Change{
					Model: "gpt-6-sol", Effort: "high", OperationKey: "initial",
				})
				require.NoError(t, err)
				version = state.Version
			}
			writer, err := db.Begin(ctx)
			require.NoError(t, err)
			defer func() { _ = writer.Rollback(ctx) }()
			var writerPID int32
			require.NoError(t, writer.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&writerPID))
			statements := map[string]string{
				"update": `UPDATE core.model_settings SET model='gpt-6-astra',effort='low',version=version+1 WHERE scope='alice'`,
				"insert": `INSERT INTO core.model_settings(scope,model,effort,version,author,authority) VALUES('alice','gpt-6-astra','low',1,'bob','others')`,
				"delete": `DELETE FROM core.model_settings WHERE scope='alice'`,
			}
			_, err = writer.Exec(ctx, statements[mutation])
			require.NoError(t, err)
			finished := make(chan error, 1)
			go func() {
				_, setErr := service.Set(ctx, "bob", "alice", modelsettings.Change{
					Model: "gpt-6-luna", Effort: "low", Version: version, OperationKey: "blocked-set",
				})
				finished <- setErr
			}()
			// Observe the real mutation waiting behind this transaction, not a timing guess.
			require.Eventually(t, func() bool {
				var blocked bool
				queryErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock'
 AND query LIKE '%core.model_settings%' AND $1=ANY(pg_blocking_pids(pid)))`, writerPID).Scan(&blocked)
				return queryErr == nil && blocked
			}, 5*time.Second, 10*time.Millisecond)
			require.NoError(t, writer.Commit(ctx))
			select {
			case err = <-finished:
				requireCode(t, err, "stale_model_settings")
			case <-ctx.Done():
				t.Fatal("model setting mutation did not finish after row writer committed")
			}
			state, err := service.Read(ctx, "bob", "alice")
			require.NoError(t, err)
			if mutation == "delete" {
				assert.Zero(t, state.Version)
				assert.Empty(t, state.Model)
			} else {
				assert.Equal(t, version+1, state.Version)
				assert.Equal(t, "gpt-6-astra", state.Model)
				assert.Equal(t, "low", state.Effort)
			}
			var recorded bool
			require.NoError(t, db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.model_setting_operations
 WHERE actor='bob' AND operation_key='blocked-set')`).Scan(&recorded))
			assert.False(t, recorded, "a stale mutation must not record a successful operation")
		})
	}
}
