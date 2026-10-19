package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func reviewWorkspaceFixture(t *testing.T) (paymentFixture, db.PaymentScope, payments.ReviewChange) {
	t.Helper()
	f := setupPayment(t, newPeers(t))
	f.H.stop(t)
	file := paymentFile(t, f.H, f.OwnerID, 19001, "workspace-proof", time.Now().UTC().Truncate(time.Second))
	e, err := f.H.store.SubmitPaymentEvidence(
		context.Background(),
		paymentScope(t, f.H, f.OwnerID, false),
		"workspace-evidence",
		payments.Submission{
			EventID:       f.Event.ID,
			PassID:        f.Pass.ID,
			FileID:        file,
			AmountMinor:   4000,
			Currency:      f.Pass.Currency,
			CurrencyScale: f.Pass.CurrencyScale,
			Purpose:       payments.Initial,
		},
	)
	require.NoError(t, err)
	return f, paymentScope(
		t,
		f.H,
		f.AmbassadorID,
		false,
	), payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  e.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
}

func workspaceCommit(w db.PaymentReviewWorkspace) db.PaymentReviewCommitChange {
	return db.PaymentReviewCommitChange{WorkspaceID: w.ID, Revision: w.Revision, Review: w.Preparation.Arguments}
}

func TestPaymentReviewWorkspaceConcurrentAliasesAndRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, review := reviewWorkspaceFixture(t)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"save-decision",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	before, err := f.H.store.PaymentBalance(ctx, paymentScope(t, f.H, f.OwnerID, false), f.Event.ID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, before.ConfirmedMinor)
	args := workspaceCommit(w)
	first, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "native-call", args)
	require.NoError(t, err)
	second, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "loom-call", args)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, w.Preparation.ExpiresAt, first.ExpiresAt)
	pending, err := f.H.store.PaymentReviewWorkspaceOperation(ctx, scope, "loom-call")
	require.NoError(t, err)
	require.False(t, pending.Completed)
	require.NotNil(t, pending.Preparation)
	require.Equal(t, second, *pending.Preparation)
	require.Equal(t, w, *pending.Workspace)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	results := make([]db.PaymentWorkspaceReviewResult, 2)
	errs := make([]error, 2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "native-call", args, first.ID)
	}()
	go func() {
		defer wg.Done()
		results[1], errs[1] = reopened.CommitPaymentReviewWorkspace(ctx, scope, "loom-call", args, second.ID)
	}()
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0], results[1])
	require.Equal(t, w.ID.String(), results[0].CanonicalOperationID)
	require.EqualValues(t, 3087, results[0].Review.Balance.ConfirmedMinor)
	var ledger, notices int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1`, review.EvidenceID).
			Scan(&ledger),
	)
	require.Equal(t, 1, ledger)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, `SELECT count(*) FROM shuttle_payment_notifications WHERE evidence_id=$1 AND kind='confirmed'`, review.EvidenceID).
			Scan(&notices),
	)
	require.Equal(t, 1, notices)
	// Read-back needs only the outer operation; it recovers the original canonical receipt.
	status, err := reopened.PaymentReviewWorkspaceOperation(ctx, scope, "loom-call")
	require.NoError(t, err)
	require.True(t, status.Completed)
	require.Equal(t, results[0], *status.Review)
	_, err = f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_payment_preparations SET expires_at=clock_timestamp()-interval '1 hour' WHERE proposal_id=$1`, w.Preparation.ID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_payment_review_calls SET expires_at=clock_timestamp()-interval '1 hour' WHERE actor_id=$1 AND kind='commit'`, scope.UserID)
	require.NoError(t, err)
	replay, err := reopened.CommitPaymentReviewWorkspace(ctx, scope, "loom-call", args, second.ID)
	require.NoError(t, err)
	require.Equal(t, results[0], replay)
	current, err := reopened.CurrentPaymentReviewWorkspace(ctx, scope, review.EventID, review.EvidenceID)
	require.NoError(t, err)
	require.Equal(t, "completed", current.State)
}

func TestPaymentReviewWorkspaceEditCancelAndOriginalReceipts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, review := reviewWorkspaceFixture(t)
	original, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare-one",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	oldArgs := workspaceCommit(original)
	oldCall, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "old-approval", oldArgs)
	require.NoError(t, err)
	review.AmountMinor = 3500
	edited, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare-two",
		db.PaymentReviewDraftChange{Review: review, ExpectedRevision: 1},
	)
	require.NoError(t, err)
	require.EqualValues(t, 2, edited.Revision)
	_, err = f.H.store.ReviewPaymentEvidence(
		ctx,
		scope,
		original.ID.String(),
		original.Preparation.Arguments,
		original.Preparation.ID,
	)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "old-approval", oldArgs, oldCall.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	stale, err := f.H.store.PaymentReviewWorkspace(ctx, scope, original.ID)
	require.NoError(t, err)
	require.False(t, stale.Current)
	args := workspaceCommit(edited)
	call, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "cancelled-approval", args)
	require.NoError(t, err)
	cancelled, err := f.H.store.CancelPaymentReviewWorkspace(
		ctx,
		scope,
		"cancel-two",
		db.PaymentReviewWorkspaceChange{WorkspaceID: args.WorkspaceID, Revision: args.Revision},
	)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)
	_, err = f.H.store.ReviewPaymentEvidence(
		ctx,
		scope,
		edited.ID.String(),
		edited.Preparation.Arguments,
		edited.Preparation.ID,
	)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "cancelled-approval", args, call.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	replacement, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare-three",
		db.PaymentReviewDraftChange{Review: review, ExpectedRevision: 2},
	)
	require.NoError(t, err)
	require.EqualValues(t, 3, replacement.Revision)
	historical, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare-one",
		db.PaymentReviewDraftChange{Review: original.Preparation.Arguments},
	)
	require.NoError(t, err)
	require.Equal(t, original, historical)
	cancelledAgain, err := f.H.store.CancelPaymentReviewWorkspace(
		ctx,
		scope,
		"cancel-two",
		db.PaymentReviewWorkspaceChange{WorkspaceID: args.WorkspaceID, Revision: args.Revision},
	)
	require.NoError(t, err)
	require.Equal(t, cancelled, cancelledAgain)
	current, err := f.H.store.CurrentPaymentReviewWorkspace(ctx, scope, review.EventID, review.EvidenceID)
	require.NoError(t, err)
	require.Equal(t, replacement, current)
	args = workspaceCommit(replacement)
	prep, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "final-approval", args)
	require.NoError(t, err)
	result, err := f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "final-approval", args, prep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3500, result.Review.Balance.ConfirmedMinor)
}

func TestPaymentReviewWorkspaceExactParametersAndExpiry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, review := reviewWorkspaceFixture(t)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	_, err = f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"stale-tab",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.ErrorIs(t, err, db.ErrConflict)
	args := workspaceCommit(w)
	prep, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "approve", args)
	require.NoError(t, err)
	modified := args
	modified.Review.AmountMinor++
	_, err = f.H.store.PreparePaymentReviewCommit(ctx, scope, "approve", modified)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "approve", modified, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "approve", args, uuid.New())
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_payment_review_calls SET expires_at=clock_timestamp()-interval '1 hour' WHERE actor_id=$1 AND operation_id='approve'`, scope.UserID)
	require.NoError(t, err)
	_, err = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "approve", args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	balance, err := f.H.store.PaymentBalance(ctx, paymentScope(t, f.H, f.OwnerID, false), review.EventID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
}

func TestPaymentReviewWorkspaceAuthorityAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, review := reviewWorkspaceFixture(t)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	args := workspaceCommit(w)
	prep, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "approve", args)
	require.NoError(t, err)
	owner := paymentScope(t, f.H, f.OwnerID, false)
	_, err = f.H.store.PaymentReviewWorkspace(ctx, owner, w.ID)
	require.Error(t, err)
	_, err = f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		owner,
		"self-confirm",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.ErrorIs(t, err, payments.ErrDenied)
	_, err = f.H.store.PaymentReviewWorkspaceOperation(ctx, owner, "prepare")
	require.Error(t, err)
	admin := paymentScope(t, f.H, f.AdminID, true)
	_, err = f.H.store.ChangeAuthority(
		ctx,
		admin.EventScope,
		admin.Subject,
		"revoke",
		db.AuthorityChange{EventID: review.EventID, UserID: scope.UserID, Role: db.AuthorityNone, ExpectedVersion: 1},
		uuid.Nil,
	)
	require.NoError(t, err)
	denied, err := f.H.store.PaymentReviewWorkspace(ctx, scope, w.ID)
	require.ErrorIs(t, err, payments.ErrDenied)
	require.Zero(t, denied)
	_, err = f.H.store.PaymentReviewWorkspaceOperation(ctx, scope, "prepare")
	require.ErrorIs(t, err, payments.ErrDenied)
	_, err = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "approve", args, prep.ID)
	require.ErrorIs(t, err, payments.ErrDenied)
}

func TestPaymentReviewWorkspaceReceiptFailureRollsBackMoneyAndNotice(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, review := reviewWorkspaceFixture(t)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	args := workspaceCommit(w)
	prep, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "approve", args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, `CREATE FUNCTION fail_review_receipt() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'fixture receipt write failure'; END$$;
 CREATE TRIGGER fail_review_receipt BEFORE UPDATE OF result ON shuttle_payment_review_calls FOR EACH ROW EXECUTE FUNCTION fail_review_receipt()`)
	require.NoError(t, err)
	_, err = f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "approve", args, prep.ID)
	require.Error(t, err)
	balance, err := f.H.store.PaymentBalance(ctx, paymentScope(t, f.H, f.OwnerID, false), review.EventID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
	require.Zero(t, balance.Version)
	var effects int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, `SELECT (SELECT count(*) FROM shuttle_payment_reviews WHERE evidence_id=$1)+(SELECT count(*) FROM shuttle_payment_notifications WHERE evidence_id=$1 AND kind='confirmed')`, review.EvidenceID).
			Scan(&effects),
	)
	require.Zero(t, effects)
	current, err := f.H.store.PaymentReviewWorkspace(ctx, scope, w.ID)
	require.NoError(t, err)
	require.Equal(t, "prepared", current.State)
	pending, err := f.H.store.PaymentReviewWorkspaceOperation(ctx, scope, "approve")
	require.NoError(t, err)
	require.False(t, pending.Completed)
	_, err = f.H.store.Pool().
		Exec(ctx, `DROP TRIGGER fail_review_receipt ON shuttle_payment_review_calls; DROP FUNCTION fail_review_receipt()`)
	require.NoError(t, err)
	result, err := f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "approve", args, prep.ID)
	require.NoError(t, err)
	require.EqualValues(t, 3087, result.Review.Balance.ConfirmedMinor)
}

func TestPaymentReviewWorkspaceRejectionCreditsNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, review := reviewWorkspaceFixture(t)
	review.Decision = payments.Rejected
	review.AmountMinor = 0
	review.Reason = "wrong recipient"
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare-reject",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	args := workspaceCommit(w)
	prep, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "reject", args)
	require.NoError(t, err)
	result, err := f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "reject", args, prep.ID)
	require.NoError(t, err)
	require.Zero(t, result.Review.Balance.ConfirmedMinor)
	require.Equal(t, payments.Rejected, result.Review.Evidence.State)
}

func TestPaymentReviewWorkspaceDirectDomainCommitPreservesSharedState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, scope, review := reviewWorkspaceFixture(t)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		ctx,
		scope,
		"prepare",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	args := workspaceCommit(w)
	prep, err := f.H.store.PreparePaymentReviewCommit(ctx, scope, "outer", args)
	require.NoError(t, err)
	domain, err := f.H.store.ReviewPaymentEvidence(ctx, scope, w.ID.String(), review, w.Preparation.ID)
	require.NoError(t, err)
	current, err := f.H.store.PaymentReviewWorkspace(ctx, scope, w.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", current.State)
	status, err := f.H.store.PaymentReviewWorkspaceOperation(ctx, scope, "outer")
	require.NoError(t, err)
	require.True(t, status.Completed)
	require.Equal(t, domain, status.Review.Review)
	var stored bool
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, `SELECT result IS NOT NULL FROM shuttle_payment_review_calls WHERE actor_id=$1 AND operation_id='outer'`, scope.UserID).
			Scan(&stored),
	)
	require.False(t, stored, "status must recover the canonical effect without writing an outer receipt")

	_, err = f.H.store.CancelPaymentReviewWorkspace(
		ctx,
		scope,
		"cancel",
		db.PaymentReviewWorkspaceChange{WorkspaceID: args.WorkspaceID, Revision: args.Revision},
	)
	require.ErrorIs(t, err, db.ErrConflict)
	// The delayed alias recovers the canonical receipt even after its proposal expiry.
	_, err = f.H.store.Pool().
		Exec(ctx, `UPDATE shuttle_payment_review_calls SET expires_at=clock_timestamp()-interval '1 hour' WHERE actor_id=$1 AND operation_id='outer'`, scope.UserID)
	require.NoError(t, err)
	result, err := f.H.store.CommitPaymentReviewWorkspace(ctx, scope, "outer", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, domain, result.Review)
	replay, err := f.H.store.ReviewPaymentEvidence(ctx, scope, w.ID.String(), review, w.Preparation.ID)
	require.NoError(t, err)
	require.Equal(t, domain, replay)
}
