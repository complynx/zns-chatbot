package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestMiniAppPairDissolutionSharesAgentReceiptAndPreservesAssignedPasses(t *testing.T) {
	t.Parallel()
	for _, first := range []string{"native", "agent"} {
		t.Run(first, func(t *testing.T) {
			t.Parallel()
			f, admin, args := acceptedPairForAssignment(t)
			ctx := t.Context()
			prep, err := f.H.store.PreparePairAssignment(ctx, admin, "membership-assign", args)
			require.NoError(t, err)
			assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "membership-assign", args, prep.ID)
			require.NoError(t, err)
			session := nativePairSession(t, f, f.A, 12002)
			request := db.PairDissolution{
				EventID:         f.EventID,
				PairID:          args.PairID,
				ExpectedVersion: assigned.Pair.Version,
			}
			var result registrations.Pair
			if first == "agent" {
				result, err = f.H.store.DissolvePair(ctx, f.A, "native-membership-opaque-key", request)
			} else {
				result, err = f.H.store.DissolveMiniAppPair(ctx, f.A, session, "native-membership-opaque-key", request)
			}
			require.NoError(t, err)
			require.Equal(t, "dissolved", result.State)
			f.H.stop(t)
			f.H.start(t)
			replay, err := f.H.store.DissolvePair(ctx, f.A, "native-membership-opaque-key", request)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			replay, err = f.H.store.DissolveMiniAppPair(ctx, f.A, session, "native-membership-opaque-key", request)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			receipt, err := f.H.store.PairDissolutionOperation(ctx, f.A, "native-membership-opaque-key")
			require.NoError(t, err)
			require.Equal(t, result, receipt)
			for _, before := range []registrations.Registration{assigned.Leader, assigned.Follower} {
				current, readErr := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, before.UserID)
				require.NoError(t, readErr)
				require.Equal(t, before.Pass, current.Pass)
				require.Equal(t, registrations.Assigned, current.State)
				require.Equal(t, before.Version+1, current.Version)
			}
			pairs, err := f.H.store.OwnedPairs(ctx, f.B, f.EventID, uuid.Nil, 20)
			require.NoError(t, err)
			require.Empty(t, pairs)
			require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, f.A.BotID, session.Bearer, session.Generation))
			_, err = f.H.store.DissolveMiniAppPair(ctx, f.A, session, "native-membership-opaque-key", request)
			require.ErrorIs(t, err, db.ErrMiniAppDenied)
			_, err = f.H.store.DissolvePair(ctx, f.A, "different-unpair", request)
			require.ErrorIs(t, err, db.ErrConflict)
		})
	}
}

func TestMiniAppPairDissolutionRechecksSessionAfterWaitingForEventLock(t *testing.T) {
	t.Parallel()
	f, _, args := acceptedPairForAssignment(t)
	ctx := t.Context()
	session := nativePairSession(t, f, f.A, 12002)
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.EventID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := f.H.store.DissolveMiniAppPair(
			ctx,
			f.A,
			session,
			"blocked-unpair",
			db.PairDissolution{EventID: f.EventID, PairID: args.PairID, ExpectedVersion: args.ExpectedPairVersion},
		)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))", blocker.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, f.A.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocker.Commit(ctx))
	select {
	case err = <-done:
		require.ErrorIs(t, err, db.ErrMiniAppDenied)
	case <-time.After(5 * time.Second):
		t.Fatal("Dissolution remained blocked after event lock release")
	}
	pairs, err := f.H.store.OwnedPairs(ctx, f.A, f.EventID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, pairs, 1)
}
