package integration_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func nativePairAssignmentSession(t *testing.T, f pairFixture, scope db.PaymentScope) db.MiniAppPairAssignmentSession {
	t.Helper()
	user, err := f.H.store.GetUser(t.Context(), scope.UserID)
	require.NoError(t, err)
	auth := nativePaymentSession(t, paymentFixture{H: f.H}, scope, user.TelegramUserID)
	return db.MiniAppPairAssignmentSession{UserID: scope.UserID, Bearer: auth.Bearer,
		Generation: auth.Generation, AuthorityUnchanged: func() bool { return true }}
}

func requireNativePairAssignmentEffects(
	t *testing.T,
	f pairFixture,
	scope db.PaymentScope,
	passes, receipts, actions int,
) {
	t.Helper()
	for _, check := range []struct {
		query string
		want  int
		args  []any
	}{
		{"SELECT count(*) FROM shuttle_passes", passes, nil},
		{"SELECT count(*) FROM shuttle_pair_operations WHERE actor=$1 AND kind='assign'", receipts, []any{scope.Subject}},
		{"SELECT count(*) FROM shuttle_miniapp_pair_assignment_actions WHERE user_id=$1", actions, []any{scope.UserID}},
	} {
		var got int
		require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), check.query, check.args...).Scan(&got))
		require.Equal(t, check.want, got, check.query)
	}
}

func TestMiniAppPairAssignmentNativeAndMixedPathsRecoverOneCanonicalReceipt(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"native", "native_to_agent", "agent_to_native"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			f, admin, args := acceptedPairForAssignment(t)
			session := nativePairAssignmentSession(t, f, admin)
			original, operation := "pair-original", "pair-original"
			var prep db.PairAssignmentPreparation
			var err error
			if path == "agent_to_native" {
				prep, err = f.H.store.PreparePairAssignment(ctx, admin, original, args)
			} else {
				prep, err = f.H.store.PrepareMiniAppPairAssignment(ctx, admin, original, args, uuid.Nil, session)
			}
			require.NoError(t, err)
			require.Len(t, prep.Quotes, 2)
			require.Equal(t, "Europe/Minsk", prep.Timezone)
			require.NotEmpty(t, prep.Description)
			switch path {
			case "agent_to_native":
				operation = "native-envelope"
				adopted, e := f.H.store.PrepareMiniAppPairAssignment(ctx, admin, operation, args, prep.ID, session)
				require.NoError(t, e)
				require.Equal(t, prep, adopted)
			case "native_to_agent":
				operation = "agent-envelope"
				adopted, e := f.H.store.AdoptPairAssignment(ctx, admin, operation, args, prep.ID)
				require.NoError(t, e)
				require.Equal(t, prep, adopted)
			}
			requireNativePairAssignmentEffects(t, f, admin, 0, 0, 1)
			var result db.PairAssignmentResult
			if path == "native_to_agent" {
				result, err = f.H.store.ConfirmPairAssignment(ctx, admin, operation, args, prep.ID)
			} else {
				result, err = f.H.store.ConfirmMiniAppPairAssignment(ctx, admin, operation, args, prep.ID, session)
			}
			require.NoError(t, err)
			require.NotNil(t, result.Leader.Pass)
			require.NotNil(t, result.Follower.Pass)
			require.NotEqual(t, result.Leader.Pass.ID, result.Follower.Pass.ID)

			// A new controller recovers an uncertain reply by reading, without repeating consent.
			reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			for _, key := range []string{original, operation} {
				view, e := reopened.MiniAppPairAssignmentProposal(ctx, admin, session, uuid.Nil, key)
				require.NoError(t, e)
				require.Equal(t, original, view.OriginalOperationID)
				require.Equal(t, prep, view.Preparation)
				require.Equal(t, db.AssignmentProposalCompleted, view.State)
				require.NotNil(t, view.Result)
				require.Equal(t, result, *view.Result)
			}
			beforeActions := 2
			if path == "native_to_agent" {
				beforeActions = 1
			}
			requireNativePairAssignmentEffects(t, f, admin, 2, 1, beforeActions)
			_, err = reopened.Pool().Exec(ctx,
				"UPDATE shuttle_pair_preparations SET expires_at=clock_timestamp()-interval '1 minute' WHERE proposal_id=$1", prep.ID)
			require.NoError(t, err)
			_, err = reopened.Pool().
				Exec(ctx, "UPDATE shuttle_events SET modules='{}',version=version+1 WHERE id=$1", args.EventID)
			require.NoError(t, err)
			view, err := reopened.MiniAppPairAssignmentProposal(ctx, admin, session, prep.ID, "")
			require.NoError(t, err)
			require.Equal(t, db.AssignmentProposalCompleted, view.State)
			require.Equal(t, prep, view.Preparation)
			replay, err := reopened.ConfirmMiniAppPairAssignment(ctx, admin, operation, args, prep.ID, session)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			replay, err = reopened.ConfirmMiniAppPairAssignment(ctx, admin, operation, args, prep.ID, session)
			require.NoError(t, err)
			require.Equal(t, result, replay)
			recent, err := reopened.RecentMiniAppPairAssignmentProposals(ctx, admin, session)
			require.NoError(t, err)
			require.Len(t, recent.Proposals, 1)
			require.Equal(t, prep.ID, recent.Proposals[0].Preparation.ID)
			requireNativePairAssignmentEffects(t, f, admin, 2, 1, 2)
		})
	}
}

func TestMiniAppPairAssignmentReadsUseOneConnectionAndDoNotGrantConsent(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	session := nativePairAssignmentSession(t, f, admin)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	prep, err := f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "one-connection", args, uuid.Nil, session)
	require.NoError(t, err)
	parsed, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", "1")
	parsed.RawQuery = query.Encode()
	limited, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	defer limited.Close()
	events, err := limited.MiniAppPairAssignmentEvents(ctx, admin, session, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	candidates, err := limited.MiniAppPairAssignmentCandidates(ctx, admin, session, args.EventID, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, candidates.Candidates, 1)
	require.Equal(t, args, candidates.Candidates[0].Arguments)
	require.Equal(t, f.A.UserID, candidates.Candidates[0].Leader.UserID)
	require.Equal(t, f.B.UserID, candidates.Candidates[0].Follower.UserID)
	for _, selector := range []struct {
		id        uuid.UUID
		operation string
	}{{prep.ID, ""}, {uuid.Nil, "one-connection"}} {
		view, e := limited.MiniAppPairAssignmentProposal(ctx, admin, session, selector.id, selector.operation)
		require.NoError(t, e)
		require.Equal(t, db.AssignmentProposalPending, view.State)
		require.Nil(t, view.Result)
		require.Equal(t, prep, view.Preparation)
	}
	pending, err := limited.PendingMiniAppPairAssignmentProposals(ctx, admin, session, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, pending.Proposals, 1)
	recent, err := limited.RecentMiniAppPairAssignmentProposals(ctx, admin, session)
	require.NoError(t, err)
	require.Len(t, recent.Proposals, 1)
	_, err = limited.MiniAppPairAssignmentProposal(ctx, admin, session, prep.ID, "one-connection")
	require.Error(t, err)
	_, err = limited.MiniAppPairAssignmentProposal(ctx, admin, session, uuid.Nil, "")
	require.Error(t, err)
	requireNativePairAssignmentEffects(t, f, admin, 0, 0, 1)
}

func TestMiniAppPairAssignmentEventAuthorityFiltersRecentBeforeLimit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	session := nativePairAssignmentSession(t, f, admin)
	prep, err := f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "authorized-older", args, uuid.Nil, session)
	require.NoError(t, err)
	other := pairAssignmentInAnotherEvent(t, f, admin)
	for i := range 21 {
		_, err = f.H.store.PrepareMiniAppPairAssignment(
			ctx,
			admin,
			fmt.Sprintf("unrelated-newer-%02d", i),
			other,
			uuid.Nil,
			session,
		)
		require.NoError(t, err)
	}
	grant, err := f.H.store.ChangeAuthority(ctx, admin.EventScope, admin.Subject, "grant-native-pair-admin",
		db.AuthorityChange{EventID: args.EventID, UserID: admin.UserID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, err)
	scoped := admin
	scoped.GlobalAdmin = false
	access, err := f.H.store.HasPairAssignmentAccess(ctx, scoped)
	require.NoError(t, err)
	require.True(t, access)
	events, err := f.H.store.MiniAppPairAssignmentEvents(ctx, scoped, session, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	recent, err := f.H.store.RecentMiniAppPairAssignmentProposals(ctx, scoped, session)
	require.NoError(t, err)
	require.Len(t, recent.Proposals, 1, "Unrelated newer actions must not consume the authorized page")
	require.Equal(t, prep.ID, recent.Proposals[0].Preparation.ID)
	assigned, err := f.H.store.ConfirmMiniAppPairAssignment(ctx, scoped, "authorized-older", args, prep.ID, session)
	require.NoError(t, err)
	require.NotNil(t, assigned.Leader.Pass)
	require.NotNil(t, assigned.Follower.Pass)
	_, err = f.H.store.MiniAppPairAssignmentCandidates(ctx, scoped, session, other.EventID, uuid.Nil)
	require.ErrorIs(t, err, db.ErrPairDenied)
	_, err = f.H.store.PrepareMiniAppPairAssignment(ctx, scoped, "out-of-scope", other, uuid.Nil, session)
	require.ErrorIs(t, err, db.ErrPairDenied)
	foreign := nativePairAssignmentSession(t, f, f.A)
	_, err = f.H.store.MiniAppPairAssignmentProposal(ctx, f.A, foreign, prep.ID, "")
	require.Error(t, err)
	_, err = f.H.store.ConfirmMiniAppPairAssignment(ctx, f.A, "authorized-older", args, prep.ID, foreign)
	require.Error(t, err)
	_, err = f.H.store.ChangeAuthority(
		ctx,
		admin.EventScope,
		admin.Subject,
		"remove-native-pair-admin",
		db.AuthorityChange{
			EventID:         args.EventID,
			UserID:          admin.UserID,
			Role:            db.AuthorityAmbassador,
			ExpectedVersion: grant.Version,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	access, err = f.H.store.HasPairAssignmentAccess(ctx, scoped)
	require.NoError(t, err)
	require.False(t, access)
	recent, err = f.H.store.RecentMiniAppPairAssignmentProposals(ctx, scoped, session)
	require.NoError(t, err)
	require.Empty(t, recent.Proposals)
	_, err = f.H.store.MiniAppPairAssignmentProposal(ctx, scoped, session, prep.ID, "")
	require.ErrorIs(t, err, db.ErrPairDenied)
	_, err = f.H.store.ConfirmMiniAppPairAssignment(ctx, scoped, "authorized-older", args, prep.ID, session)
	require.ErrorIs(t, err, db.ErrPairDenied)
	requireNativePairAssignmentEffects(t, f, admin, 2, 1, 23)
}

func TestMiniAppPairAssignmentRejectsSessionLossBeforeEffectsAndReceiptReplay(t *testing.T) {
	t.Parallel()
	for _, completed := range []bool{false, true} {
		for _, failure := range []string{"generation", "logout", "expired", "nil_authority", "invalidated_authority"} {
			t.Run(fmt.Sprintf("completed_%t/%s", completed, failure), func(t *testing.T) {
				t.Parallel()
				ctx := t.Context()
				f, admin, args := acceptedPairForAssignment(t)
				session := nativePairAssignmentSession(t, f, admin)
				prep, err := f.H.store.PreparePairAssignment(ctx, admin, "session-loss", args)
				require.NoError(t, err)
				if completed {
					_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "session-loss", args, prep.ID)
					require.NoError(t, err)
				}
				switch failure {
				case "generation":
					session.Generation = sha256.Sum256([]byte("wrong-generation"))
				case "logout":
					require.NoError(
						t,
						f.H.store.LogoutMiniAppSession(ctx, admin.BotID, session.Bearer, session.Generation),
					)
				case "expired":
					_, err = f.H.store.Pool().
						Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE bearer_hash=$1", session.Bearer[:])
					require.NoError(t, err)
				case "nil_authority":
					session.AuthorityUnchanged = nil
				case "invalidated_authority":
					session.AuthorityUnchanged = func() bool { return false }
				}
				_, err = f.H.store.PrepareMiniAppPairAssignment(ctx, admin, "session-loss", args, uuid.Nil, session)
				require.Error(t, err)
				_, err = f.H.store.ConfirmMiniAppPairAssignment(ctx, admin, "session-loss", args, prep.ID, session)
				require.Error(t, err)
				_, err = f.H.store.MiniAppPairAssignmentProposal(ctx, admin, session, prep.ID, "")
				require.Error(t, err)
				passes, receipts := 0, 0
				if completed {
					passes, receipts = 2, 1
				}
				requireNativePairAssignmentEffects(t, f, admin, passes, receipts, 0)
			})
		}
	}
}

func TestMiniAppPairAssignmentRechecksSessionAfterLockWait(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"event", "proposal"} {
		failures := []string{"invalidated", "expired"}
		if target == "event" {
			failures = append(failures, "logout")
		}
		for _, failure := range failures {
			t.Run(target+"/"+failure, func(t *testing.T) {
				t.Parallel()
				testMiniAppPairAssignmentSessionAfterLockWait(t, target, failure)
			})
		}
	}
}

func testMiniAppPairAssignmentSessionAfterLockWait(t *testing.T, target, failure string) {
	t.Helper()
	f, admin, args := acceptedPairForAssignment(t)
	session := nativePairAssignmentSession(t, f, admin)
	var valid atomic.Bool
	valid.Store(true)
	session.AuthorityUnchanged = valid.Load
	prep, err := f.H.store.PreparePairAssignment(t.Context(), admin, "blocked-native", args)
	require.NoError(t, err)
	blocker, err := f.H.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	switch target {
	case "event":
		_, err = blocker.Exec(t.Context(), "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", args.EventID)
	case "proposal":
		_, err = blocker.Exec(
			t.Context(),
			"SELECT proposal_id FROM shuttle_pair_preparations WHERE proposal_id=$1 FOR UPDATE",
			prep.ID,
		)
	}
	require.NoError(t, err)
	var expiry time.Time
	if failure == "expired" {
		err = f.H.store.Pool().QueryRow(t.Context(),
			"UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE bearer_hash=$1 RETURNING expires_at", session.Bearer[:]).
			Scan(&expiry)
		require.NoError(t, err)
	}
	// Bound the contested operation after fixture and blocker setup, not before it.
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, e := f.H.store.ConfirmMiniAppPairAssignment(ctx, admin, "blocked-native", args, prep.ID, session)
		finished <- e
	}()
	var probeErr error
	require.Eventually(t, func() bool {
		// Use the blocker connection so observation does not compete for a pool lease.
		_, probeErr = blocker.Exec(ctx, "SELECT pg_stat_clear_snapshot()")
		if probeErr != nil {
			return true
		}
		var waiting bool
		probeErr = blocker.QueryRow(
			ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)))",
			blocker.Conn().PgConn().PID(),
		).Scan(&waiting)
		return probeErr != nil || waiting
	}, 3*time.Second, 10*time.Millisecond)
	require.NoError(t, probeErr, "Observe confirmation waiting on the held lock")
	switch failure {
	case "invalidated":
		valid.Store(false)
	case "logout":
		require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, admin.BotID, session.Bearer, session.Generation))
	case "expired":
		require.Eventually(t, func() bool {
			var expired bool
			probeErr = blocker.QueryRow(ctx, "SELECT clock_timestamp()>$1::timestamptz", expiry).Scan(&expired)
			return probeErr != nil || expired
		}, 3*time.Second, 10*time.Millisecond)
		require.NoError(t, probeErr, "Observe session expiry using the blocker connection")
	}
	require.NoError(t, blocker.Commit(ctx))
	select {
	case err = <-finished:
		expected := db.ErrMiniAppDenied
		if failure == "invalidated" {
			expected = db.ErrPairDenied
		}
		require.ErrorIs(t, err, expected, "A session denial must not be mistaken for a test deadline")
	case <-ctx.Done():
		t.Fatal("Pair confirmation did not finish after lock release")
	}
	requireNativePairAssignmentEffects(t, f, admin, 0, 0, 0)
}
