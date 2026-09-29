package integration_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/jackc/pgx/v5"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfirmationRechecksTimeAfterSlotLock(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"intent_expired", "slot_closed"} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			s := workflow.Service{DB: db}
			mustExec(t, s, "alice", action("select", "massage-1", 0, "select", "manual"))
			var deadline time.Time
			query := `UPDATE core.workflows SET expires_at=clock_timestamp()+interval '2 seconds' WHERE owner='alice' RETURNING expires_at`
			if code == "slot_closed" {
				query = `UPDATE core.slots SET starts_at=clock_timestamp()+interval '2 seconds' WHERE id='massage-1' RETURNING starts_at`
			}
			require.NoError(t, db.QueryRow(t.Context(), query).Scan(&deadline))
			lock, err := db.Begin(t.Context())
			require.NoError(t, err)
			t.Cleanup(func() { _ = lock.Rollback(context.WithoutCancel(t.Context())) })
			_, err = lock.Exec(t.Context(), `SELECT id FROM core.slots WHERE id='massage-1' FOR UPDATE`)
			require.NoError(t, err)
			var clock atomic.Int64
			clock.Store(deadline.Add(-time.Second).UnixNano())
			s.Clock = func(context.Context, pgx.Tx) (time.Time, error) {
				return time.Unix(0, clock.Load()), nil
			}
			result := make(chan error, 1)
			go func() {
				_, confirmError := s.Execute(t.Context(), "alice", action("confirm", "", 1, "confirm", "manual"))
				result <- confirmError
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				err = db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
					WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM core.slots%FOR UPDATE%')`).Scan(&waiting)
				return err == nil && waiting
			}, time.Second, 10*time.Millisecond, "confirmation must wait for the slot row")
			clock.Store(deadline.Add(time.Second).UnixNano())
			require.NoError(t, lock.Commit(t.Context()))
			select {
			case err = <-result:
				requireCode(t, err, code)
			case <-time.After(5 * time.Second):
				t.Fatal("confirmation did not finish")
			}
			var state string
			var version int64
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT state,version FROM core.workflows WHERE owner='alice'`).
					Scan(&state, &version),
			)
			assert.Equal(t, "draft", state)
			assert.EqualValues(t, 1, version)
		})
	}
}
