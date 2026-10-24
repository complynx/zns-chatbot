package integration_test

import (
	"crypto/sha256"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMiniAppMealOrderSharedHandoffsAndCancellation(t *testing.T) {
	t.Parallel()
	f, scope, args := mealSubmissionFixture(t)
	ctx := t.Context()
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	session := nativeOrderSession(t, f, scope, 11002)
	nativeOp, agentOp := uuid.New(), uuid.New()
	native, err := f.H.store.PrepareMiniAppMealOrderSubmission(ctx, deployment, session, nativeOp, args)
	require.NoError(t, err)
	agent, err := f.H.store.PrepareOwnMealOrderSubmission(ctx, scope, agentOp, args, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, native, agent)
	first, err := f.H.store.SubmitOwnMealOrder(ctx, scope, agentOp, args, agent.ID, db.OrderSourceAgent)
	require.NoError(t, err)
	alias := uuid.New()
	recovered, err := f.H.store.SubmitMiniAppMealOrder(ctx, deployment, session, alias, args, native.ID)
	require.NoError(t, err)
	require.Equal(t, first, recovered)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	recovered, err = reopened.SubmitMiniAppMealOrder(ctx, deployment, session, alias, args, native.ID)
	require.NoError(t, err)
	require.Equal(t, first, recovered)
	cancelOp := uuid.New()
	cancellation := db.OrderCancellation{EventID: args.EventID, OrderID: first.Order.ID, ExpectedVersion: 1}
	cancelled, err := reopened.CancelMiniAppMealOrder(ctx, deployment, session, cancelOp, cancellation)
	require.NoError(t, err)
	require.Equal(t, db.OrderKindMeal, cancelled.OrderKind)
	require.Equal(t, "cancelled", cancelled.State)
	replay, err := reopened.CancelOwnMealOrder(ctx, scope, cancelOp, cancellation, db.OrderSourceAgent)
	require.NoError(t, err)
	require.Equal(t, cancelled, replay)
	original, err := reopened.SubmitMiniAppMealOrder(ctx, deployment, session, alias, args, native.ID)
	require.NoError(t, err)
	require.Equal(t, first, original, "Submission receipt remains the original result after cancellation")
	current, err := reopened.OwnMealOrder(ctx, scope, args.EventID, first.Order.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled, current)
	mealOrderCounts(t, f, 1, 1)
}

func TestMiniAppMealOrderSessionIsRequiredBeforeReceiptReplay(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "expiry", "generation", "foreign-owner", "foreign-organization"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, scope, args := mealSubmissionFixture(t)
			ctx := t.Context()
			deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
			session := nativeOrderSession(t, f, scope, 11002)
			operation := uuid.New()
			prepared, err := f.H.store.PrepareMiniAppMealOrderSubmission(ctx, deployment, session, operation, args)
			require.NoError(t, err)
			result, err := f.H.store.SubmitMiniAppMealOrder(ctx, deployment, session, operation, args, prepared.ID)
			require.NoError(t, err)
			switch kind {
			case "logout":
				require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
			case "expiry":
				_, err = f.H.store.Pool().
					Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interva" +
"l '1 second' WHERE bearer_hash=$1", session.Bearer[:])
				require.NoError(t, err)
			case "generation":
				session.Generation = sha256.Sum256([]byte("other-native-order-key"))
			case "foreign-owner":
				session = nativeOrderSession(t, f, ownerProfileScope(t, f.H, f.AmbassadorID), 11003)
			case "foreign-organization":
				deployment.Organization = "foreign-org"
			}
			_, err = f.H.store.PrepareMiniAppMealOrderSubmission(ctx, deployment, session, operation, args)
			require.Error(t, err)
			_, err = f.H.store.SubmitMiniAppMealOrder(ctx, deployment, session, operation, args, prepared.ID)
			require.Error(t, err)
			_, err = f.H.store.CancelMiniAppMealOrder(
				ctx,
				deployment,
				session,
				uuid.New(),
				db.OrderCancellation{EventID: args.EventID, OrderID: result.Order.ID, ExpectedVersion: 1},
			)
			require.Error(t, err)
			current, err := f.H.store.OwnMealOrder(ctx, scope, args.EventID, result.Order.ID)
			require.NoError(t, err)
			require.Equal(t, "submitted", current.State)
			var count int
			require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
			require.Equal(t, 1, count)
		})
	}
}

func TestMiniAppMealOrderRechecksSessionAfterEventLockWait(t *testing.T) {
	t.Parallel()
	f, scope, args := mealSubmissionFixture(t)
	ctx := t.Context()
	deployment := db.EventScope{BotID: scope.BotID, Organization: scope.Organization}
	session := nativeOrderSession(t, f, scope, 11002)
	operation := uuid.New()
	prepared, err := f.H.store.PrepareMiniAppMealOrderSubmission(ctx, deployment, session, operation, args)
	require.NoError(t, err)
	blocking, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(ctx) }()
	_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", args.EventID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := f.H.store.SubmitMiniAppMealOrder(ctx, deployment, session, operation, args, prepared.ID)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid" +
")))", blocking.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	// Logout does not need the event lock, and must complete before the blocked
	// write is released. The write must then reject this now-invalid session.
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocking.Commit(ctx))
	select {
	case e := <-done:
		require.ErrorIs(t, e, db.ErrMiniAppDenied)
	case <-time.After(5 * time.Second):
		t.Fatal("Blocked native submission did not finish")
	}
	saved, err := f.H.store.OwnMealDraft(ctx, scope, args.EventID)
	require.NoError(t, err)
	require.Equal(t, int64(1), saved.Version)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_orders").Scan(&count))
	require.Zero(t, count)
}
