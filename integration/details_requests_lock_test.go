package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

func TestDetailsRequestBeginAndOwnAdminEventConfirmationDoNotDeadlock(t *testing.T) {
	t.Parallel()
	h, scope, event := detailsRequestFixture(t)
	admin := db.AdminScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		Subject:      scope.Subject,
		UserID:       scope.UserID,
		GlobalAdmin:  true,
	}
	args := db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document}
	args.Document.Name += " admin-confirmed"
	operation := uuid.NewString()
	prep, err := h.store.PrepareEventChange(t.Context(), admin, operation, args)
	require.NoError(t, err)
	control, err := h.store.Pool().Acquire(t.Context())
	require.NoError(t, err)
	tx, err := control.Begin(t.Context())
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	var workers sync.WaitGroup
	t.Cleanup(func() {
		cancel()
		cleanup, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		_ = tx.Rollback(cleanup)
		_, _ = control.Exec(cleanup, `SELECT pg_advisory_unlock_all()`)
		control.Release()
		workers.Wait()
	})
	_, err = tx.Exec(ctx, `SELECT id FROM shuttle_users WHERE id=$1 FOR SHARE`, scope.UserID)
	require.NoError(t, err)
	gate := "details-requests:" + scope.UserID.String()
	_, err = tx.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, gate)
	require.NoError(t, err)
	type beginOutcome struct {
		result db.DetailsRequestStatus
		err    error
	}
	began := make(chan beginOutcome, 1)
	workers.Go(func() {
		result, e := h.store.BeginDetailsRequest(
			ctx,
			scope,
			uuid.New(),
			db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version},
		)
		began <- beginOutcome{result, e}
	})
	// Observe the blocking relation, not whether the writer first uses SELECT or UPDATE.
	var beginPID uint32
	require.Eventually(t, func() bool {
		e := h.store.Pool().
			QueryRow(ctx,
				"SELECT pid FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) LIMIT 1",
				control.Conn().PgConn().PID()).
			Scan(&beginPID)
		return e == nil
	}, 3*time.Second, 10*time.Millisecond)
	type confirmOutcome struct {
		result events.Event
		err    error
	}
	confirmed := make(chan confirmOutcome, 1)
	workers.Go(func() {
		result, e := h.store.ConfirmEventChange(ctx, admin, operation, args, prep.ID)
		confirmed <- confirmOutcome{result, e}
	})
	require.Eventually(t, func() bool {
		var waiting bool
		e := h.store.Pool().
			QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, beginPID).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	_, err = tx.Exec(ctx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, gate)
	require.NoError(t, err)
	select {
	case outcome := <-began:
		require.NoError(t, outcome.err)
		require.Equal(t, event.Version, outcome.result.Request.EventVersion)
	case <-ctx.Done():
		t.Fatal("Details request remained blocked")
	}
	select {
	case outcome := <-confirmed:
		require.NoError(t, outcome.err)
		require.Equal(t, event.Version+1, outcome.result.Version)
	case <-ctx.Done():
		t.Fatal("Event confirmation remained blocked")
	}
}
