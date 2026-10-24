package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func orderReviewFixture(t *testing.T) (paymentFixture, db.PaymentScope, db.OrderEvidenceReview) {
	t.Helper()
	f, owner, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61401, "shared-order-review")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), owner, uuid.New(), args)
	require.NoError(t, err)
	f.H.stop(t)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	return f, paymentScope(
		t,
		f.H,
		f.AmbassadorID,
		false,
	), db.OrderEvidenceReview{
		EventID:         f.Event.ID,
		EvidenceID:      evidence.ID,
		ExpectedVersion: 1,
		Decision:        db.OrderEvidenceConfirmed,
		Reason:          "Synthetic verified order transfer",
	}
}

func orderReviewCommit(w db.OrderReviewWorkspace) db.OrderReviewCommitChange {
	return db.OrderReviewCommitChange{WorkspaceID: w.ID, Revision: w.Revision, Review: w.Preparation.Arguments}
}

func TestOrderReviewWorkspaceHandoffsCanonicalRecoveryAndRestart(t *testing.T) {
	t.Parallel()
	for _, start := range []string{"agent", "native"} {
		t.Run(start, func(t *testing.T) {
			t.Parallel()
			f, scope, review := orderReviewFixture(t)
			session := nativePaymentSession(t, f, scope, 11003)
			save := uuid.New()
			var w db.OrderReviewWorkspace
			var err error
			if start == "native" {
				w, err = f.H.store.PrepareMiniAppOrderReviewWorkspace(
					t.Context(),
					scope,
					session,
					save,
					db.OrderReviewDraftChange{Review: review},
				)
			} else {
				w, err = f.H.store.PrepareOrderReviewWorkspace(
					t.Context(),
					scope,
					save,
					db.OrderReviewDraftChange{Review: review},
				)
			}
			require.NoError(t, err)
			current, err := f.H.store.CurrentMiniAppOrderReviewWorkspace(
				t.Context(),
				scope,
				session,
				review.EventID,
				review.EvidenceID,
			)
			require.NoError(t, err)
			require.Equal(t, w, current)
			args := orderReviewCommit(w)
			agentOp, nativeOp := uuid.New(), uuid.New()
			prep, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, agentOp, args)
			require.NoError(t, err)
			pending, err := f.H.store.OrderReviewOperationByID(t.Context(), scope, agentOp)
			require.NoError(t, err)
			require.False(t, pending.Completed)
			require.Equal(t, prep, *pending.Preparation)
			require.Nil(t, pending.Review)
			var first db.OrderWorkspaceReviewResult
			if start == "native" {
				first, err = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, agentOp, args, prep.ID)
			} else {
				first, err = f.H.store.CommitMiniAppOrderReviewWorkspace(
					t.Context(),
					scope,
					session,
					nativeOp,
					args,
					w.Preparation.ID,
				)
			}
			require.NoError(t, err)
			reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
			require.NoError(t, err)
			defer reopened.Close()
			// Recovery must precede freshness checks; both canonical columns and the
			// saved presentation timestamp are deliberately expired after completion.
			_, err = reopened.Pool().
				Exec(t.Context(), "UPDATE shuttle_order_evidence_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour',document=jsonb_set(document,'{expires_at}',to_json" +
"b(clock_timestamp()-interval '1 hour')) WHERE id=$1", w.Preparation.ID)
			require.NoError(t, err)
			agent, err := reopened.CommitOrderReviewWorkspace(t.Context(), scope, agentOp, args, prep.ID)
			require.NoError(t, err)
			native, err := reopened.CommitMiniAppOrderReviewWorkspace(
				t.Context(),
				scope,
				session,
				nativeOp,
				args,
				w.Preparation.ID,
			)
			require.NoError(t, err)
			require.Equal(t, first, agent)
			require.Equal(t, first, native)
			require.Equal(t, w.ID, first.CanonicalOperationID)
			require.Equal(t, db.OrderEvidenceConfirmed, first.Evidence.State)
			require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
			current, err = reopened.CurrentOrderReviewWorkspace(t.Context(), scope, review.EventID, review.EvidenceID)
			require.NoError(t, err)
			require.Equal(t, "completed", current.State)
			for _, op := range []uuid.UUID{agentOp, nativeOp} {
				receipt, readErr := reopened.OrderReviewOperationByID(t.Context(), scope, op)
				require.NoError(t, readErr)
				require.True(t, receipt.Completed)
				require.Equal(t, first, *receipt.Review)
			}
			// Transport writes cannot claim the other source's operation ID.
			if start == "native" {
				_, err = reopened.PrepareOrderReviewWorkspace(
					t.Context(),
					scope,
					save,
					db.OrderReviewDraftChange{Review: review},
				)
			} else {
				_, err = reopened.PrepareMiniAppOrderReviewWorkspace(
					t.Context(),
					scope,
					session,
					save,
					db.OrderReviewDraftChange{Review: review},
				)
			}
			require.ErrorIs(t, err, db.ErrConflict)
			var reviews, receipts int
			require.NoError(
				t,
				reopened.Pool().
					QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_order_evidence_reviews WHERE eviden" +
"ce_id=$1),(SELECT count(*) FROM shuttle_order_evidence_operations WHERE " +
"operation_id=$2 AND kind='review')", review.EvidenceID, w.ID).
					Scan(&reviews, &receipts),
			)
			require.Equal(t, 1, reviews)
			require.Equal(t, 1, receipts)
		})
	}
}

func TestOrderReviewWorkspaceEditsAndCancelInvalidateOldConsent(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	op := uuid.New()
	args := orderReviewCommit(w)
	prepared, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, op, args)
	require.NoError(t, err)
	changed := args
	changed.Review.Reason = "A different decision"
	_, err = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, changed, prepared.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, args, uuid.New())
	require.ErrorIs(t, err, db.ErrConflict)
	edited := review
	edited.Reason = "Updated human intent"
	replacement, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: edited, ExpectedRevision: 1},
	)
	require.NoError(t, err)
	require.Equal(t, int64(2), replacement.Revision)
	_, err = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, args, prepared.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	old, err := f.H.store.OrderReviewWorkspaceByID(t.Context(), scope, w.ID)
	require.NoError(t, err)
	require.False(t, old.Current)
	require.Equal(t, review, old.Preparation.Arguments)
	cancelled, err := f.H.store.CancelOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewWorkspaceChange{WorkspaceID: replacement.ID, Revision: 2},
	)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)
	_, err = f.H.store.PrepareOrderReviewCommit(t.Context(), scope, uuid.New(), orderReviewCommit(replacement))
	require.ErrorIs(t, err, db.ErrConflict)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
}

func TestOrderReviewWorkspaceAtomicRollbackIncludesAliasAndCompletion(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"decision", "workspace", "receipt"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			f, scope, review := orderReviewFixture(t)
			w, err := f.H.store.PrepareOrderReviewWorkspace(
				t.Context(),
				scope,
				uuid.New(),
				db.OrderReviewDraftChange{Review: review},
			)
			require.NoError(t, err)
			op := uuid.New()
			args := orderReviewCommit(w)
			prep, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, op, args)
			require.NoError(t, err)
			event, table := "INSERT", "shuttle_order_evidence_reviews"
			if failure == "workspace" {
				event, table = "UPDATE", "shuttle_order_review_workspaces"
			}
			if failure == "receipt" {
				event, table = "UPDATE OF result", "shuttle_order_review_calls"
			}
			_, err = f.H.store.Pool().
				Exec(t.Context(), "CREATE FUNCTION fail_shared_order_review() RETURNS trigger LANGUAGE plpg" +
"sql AS $$BEGIN RAISE EXCEPTION 'synthetic shared order review failure'; " +
"END$$; CREATE TRIGGER fail_shared_order_review BEFORE "+event+" ON "+table+" FOR EACH ROW EXECUTE FUNCTION fail_shared_order_review()")
			require.NoError(t, err)
			_, err = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, args, prep.ID)
			require.Error(t, err)
			current, err := f.H.store.CurrentOrderReviewWorkspace(t.Context(), scope, review.EventID, review.EvidenceID)
			require.NoError(t, err)
			require.Equal(t, "prepared", current.State)
			pending, err := f.H.store.OrderReviewOperationByID(t.Context(), scope, op)
			require.NoError(t, err)
			require.False(t, pending.Completed)
			require.Nil(t, pending.Review)
			var count int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), "SELECT count(*) FROM shuttle_order_evidence_reviews WHERE evidence_id=$1", review.EvidenceID).
					Scan(&count),
			)
			require.Zero(t, count)
			require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
			_, err = f.H.store.Pool().
				Exec(t.Context(), "DROP TRIGGER fail_shared_order_review ON "+table+"; DROP FUNCTION fail_shared_order_review()")
			require.NoError(t, err)
			result, err := f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, args, prep.ID)
			require.NoError(t, err)
			require.Equal(t, db.OrderEvidenceConfirmed, result.Evidence.State)
		})
	}
}

func TestOrderReviewWorkspaceConcurrentReviewersDecideOnce(t *testing.T) {
	t.Parallel()
	f, amb, review := orderReviewFixture(t)
	scopes := []db.PaymentScope{amb, paymentScope(t, f.H, f.AdminID, true)}
	workspaces := make([]db.OrderReviewWorkspace, 2)
	operations := []uuid.UUID{uuid.New(), uuid.New()}
	proposals := make([]uuid.UUID, 2)
	for i, scope := range scopes {
		var err error
		workspaces[i], err = f.H.store.PrepareOrderReviewWorkspace(
			t.Context(),
			scope,
			uuid.New(),
			db.OrderReviewDraftChange{Review: review},
		)
		require.NoError(t, err)
		prepared, prepErr := f.H.store.PrepareOrderReviewCommit(
			t.Context(),
			scope,
			operations[i],
			orderReviewCommit(workspaces[i]),
		)
		require.NoError(t, prepErr)
		proposals[i] = prepared.ID
	}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, scope := range scopes {
		wg.Go(func() {
			_, errs[i] = f.H.store.CommitOrderReviewWorkspace(
				t.Context(),
				scope,
				operations[i],
				orderReviewCommit(workspaces[i]),
				proposals[i],
			)
		})
	}
	wg.Wait()
	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			require.ErrorIs(t, err, db.ErrConflict)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
}

func TestOrderReviewWorkspaceNativeSessionRecheckedAfterEventWait(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	blocker, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	_, err = blocker.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, writeErr := f.H.store.CommitMiniAppOrderReviewWorkspace(
			ctx,
			scope,
			session,
			uuid.New(),
			orderReviewCommit(w),
			w.Preparation.ID,
		)
		done <- writeErr
	}()
	require.Eventually(t, func() bool {
		var count int
		readErr := f.H.store.Pool().
			QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_" +
"pids(pid))", blocker.Conn().PgConn().PID()).
			Scan(&count)
		return readErr == nil && count > 0
	}, 3*time.Second, 20*time.Millisecond)
	require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, scope.BotID, session.Bearer, session.Generation))
	require.NoError(t, blocker.Rollback(ctx))
	require.ErrorIs(t, <-done, db.ErrMiniAppDenied)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	require.Zero(t, orderAllocationUnits(t, f, "occupied"))
}

func TestOrderReviewWorkspaceCurrentRightsRequiredForCompletedReplay(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	op := uuid.New()
	_, err = f.H.store.CommitMiniAppOrderReviewWorkspace(
		t.Context(),
		scope,
		session,
		op,
		orderReviewCommit(w),
		w.Preparation.ID,
	)
	require.NoError(t, err)
	require.NoError(t, f.H.store.LogoutMiniAppSession(t.Context(), scope.BotID, session.Bearer, session.Generation))
	_, err = f.H.store.MiniAppOrderReviewOperationByID(t.Context(), scope, session, op)
	require.ErrorIs(t, err, db.ErrMiniAppDenied)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, scope.UserID)
	require.NoError(t, err)
	_, err = f.H.store.OrderReviewOperationByID(t.Context(), scope, op)
	require.Error(t, err)
	_, err = f.H.store.CommitMiniAppOrderReviewWorkspace(
		t.Context(),
		scope,
		session,
		op,
		orderReviewCommit(w),
		w.Preparation.ID,
	)
	require.Error(t, err)
}

func TestOrderReviewWorkspaceConcurrentTransportAliasesAgree(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	args := orderReviewCommit(w)
	op := uuid.New()
	prep, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, op, args)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]db.OrderWorkspaceReviewResult, 2)
	errs := make([]error, 2)
	wg.Go(func() {
		results[0], errs[0] = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, args, prep.ID)
	})
	wg.Go(func() {
		results[1], errs[1] = f.H.store.CommitMiniAppOrderReviewWorkspace(
			t.Context(),
			scope,
			session,
			uuid.New(),
			args,
			w.Preparation.ID,
		)
	})
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0], results[1])
	require.Equal(t, w.ID, results[0].CanonicalOperationID)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "occupied"))
}

func TestOrderReviewNativeReceiptFailureRollsBackNewAlias(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_native_order_alias() RETURNS trigger LANGUAGE plpgs" +
"ql AS $$BEGIN RAISE EXCEPTION 'synthetic native alias failure'; END$$; C" +
"REATE TRIGGER fail_native_order_alias BEFORE UPDATE OF result ON shuttle" +
"_order_review_calls FOR EACH ROW EXECUTE FUNCTION fail_native_order_alia" +
"s()")
	require.NoError(t, err)
	op := uuid.New()
	_, err = f.H.store.CommitMiniAppOrderReviewWorkspace(
		t.Context(),
		scope,
		session,
		op,
		orderReviewCommit(w),
		w.Preparation.ID,
	)
	require.Error(t, err)
	var calls, reviews int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_order_review_calls WHERE operation_" +
"id=$1),(SELECT count(*) FROM shuttle_order_evidence_reviews WHERE eviden" +
"ce_id=$2)", op, review.EvidenceID).
			Scan(&calls, &reviews),
	)
	require.Zero(t, calls)
	require.Zero(t, reviews)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER fail_native_order_alias ON shuttle_order_review_calls; DROP" +
" FUNCTION fail_native_order_alias()")
	require.NoError(t, err)
	result, err := f.H.store.CommitMiniAppOrderReviewWorkspace(
		t.Context(),
		scope,
		session,
		op,
		orderReviewCommit(w),
		w.Preparation.ID,
	)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidenceConfirmed, result.Evidence.State)
}

func TestOrderReviewWorkspaceStandaloneCannotBypassSharedIntent(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"cancelled", "replaced"} {
		for _, entry := range []string{"canonical", "older_standalone"} {
			t.Run(state+"/"+entry, func(t *testing.T) {
				t.Parallel()
				f, scope, review := orderReviewFixture(t)
				legacyOp := uuid.New()
				legacy, err := f.H.store.PrepareOrderEvidenceReview(t.Context(), scope, legacyOp, review)
				require.NoError(t, err)
				w, err := f.H.store.PrepareOrderReviewWorkspace(
					t.Context(),
					scope,
					uuid.New(),
					db.OrderReviewDraftChange{Review: review},
				)
				require.NoError(t, err)
				if state == "cancelled" {
					_, err = f.H.store.CancelOrderReviewWorkspace(
						t.Context(),
						scope,
						uuid.New(),
						db.OrderReviewWorkspaceChange{WorkspaceID: w.ID, Revision: 1},
					)
				} else {
					edited := review
					edited.Reason = "Replacement review"
					_, err = f.H.store.PrepareOrderReviewWorkspace(
						t.Context(),
						scope,
						uuid.New(),
						db.OrderReviewDraftChange{Review: edited, ExpectedRevision: 1},
					)
				}
				require.NoError(t, err)
				op, proposal := w.ID, w.Preparation.ID
				if entry == "older_standalone" {
					op, proposal = legacyOp, legacy.ID
				}
				_, err = f.H.store.ReviewOrderEvidence(t.Context(), scope, op, proposal, review)
				require.ErrorIs(
					t,
					err,
					db.ErrConflict,
					"Standalone entry cannot consume replaced/cancelled shared consent",
				)
				_, err = f.H.store.PrepareOrderEvidenceReview(t.Context(), scope, uuid.New(), review)
				require.ErrorIs(t, err, db.ErrConflict, "New standalone intent must not bypass the shared workflow")
				require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
				require.Zero(t, orderAllocationUnits(t, f, "occupied"))
			})
		}
	}
}

func TestOrderReviewWorkspaceExpiredConsentCannotStartNewDecision(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	session := nativePaymentSession(t, f, scope, 11003)
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	op := uuid.New()
	prep, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, op, orderReviewCommit(w))
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_order_evidence_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour',document=jsonb_set(document,'{expires_at}',to_json" +
"b(clock_timestamp()-interval '1 hour')) WHERE id=$1", w.Preparation.ID)
	require.NoError(t, err)
	_, err = f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, orderReviewCommit(w), prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.CommitMiniAppOrderReviewWorkspace(
		t.Context(),
		scope,
		session,
		uuid.New(),
		orderReviewCommit(w),
		w.Preparation.ID,
	)
	require.ErrorIs(t, err, db.ErrConflict)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
	current, err := f.H.store.CurrentOrderReviewWorkspace(t.Context(), scope, review.EventID, review.EvidenceID)
	require.NoError(t, err)
	require.Equal(t, "prepared", current.State)
}

func TestOrderReviewWorkspaceRevokedPayerCanStillBeRejected(t *testing.T) {
	t.Parallel()
	f, scope, review := orderReviewFixture(t)
	review.Decision = db.OrderEvidenceRejected
	w, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	op := uuid.New()
	prep, err := f.H.store.PrepareOrderReviewCommit(t.Context(), scope, op, orderReviewCommit(w))
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	result, err := f.H.store.CommitOrderReviewWorkspace(t.Context(), scope, op, orderReviewCommit(w), prep.ID)
	require.NoError(t, err)
	require.Equal(t, db.OrderEvidenceRejected, result.Evidence.State)
	require.Equal(t, review.Reason, result.Evidence.Review.Reason)
	require.Zero(t, orderAllocationUnits(t, f, "held"))
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "released"))
}
