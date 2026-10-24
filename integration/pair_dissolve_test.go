package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestPairDissolutionReleasesWaitlistsWithoutAdministratorImpersonation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	preparedPair, err := f.H.store.PreparePairAssignment(ctx, admin, "stale-pair-assignment", args)
	require.NoError(t, err)
	request := db.PairDissolution{EventID: f.EventID, PairID: args.PairID, ExpectedVersion: args.ExpectedPairVersion}
	_, err = f.H.store.DissolvePair(ctx, admin, "admin-unpair", request)
	require.ErrorIs(t, err, db.ErrPairDenied)
	_, err = f.H.store.DissolvePair(ctx, f.Other, "other-unpair", request)
	require.ErrorIs(t, err, db.ErrPairDenied)
	_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_events SET modules='{}' WHERE id=$1", f.EventID)
	require.NoError(t, err)
	require.NoError(t, f.H.store.RevokeUser(ctx, f.B.UserID, true))
	result, err := f.H.store.DissolvePair(ctx, f.A, "unpair", request)
	require.NoError(t, err)
	require.Equal(t, "dissolved", result.State)
	require.NotNil(t, result.DissolvedAt)
	require.Nil(t, result.AssignedAt)
	require.Equal(t, request.ExpectedVersion+1, result.Version)
	leader, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	require.Equal(t, f.Entry.RequestedAt, leader.RequestedAt)
	require.Equal(t, registrations.Waitlisted, leader.State)
	require.Nil(t, leader.Pass)
	require.Equal(t, args.ExpectedLeaderVersion+1, leader.Version)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_events SET modules=ARRAY['registrations','orders'] WHERE id=$1", f.EventID)
	require.NoError(t, err)
	_, err = f.H.store.PrepareAssignment(ctx, admin.EventScope, admin.Subject, "solo-after-unpair", db.PassAssignment{
		EventID: f.EventID, RegistrationID: leader.ID, ExpectedEventVersion: 1, ExpectedVersion: leader.Version,
	})
	require.NoError(t, err)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "stale-pair-assignment", args, preparedPair.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	f.H.stop(t)
	f.H.start(t)
	replay, err := f.H.store.DissolvePair(ctx, f.A, "unpair", request)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	_, err = f.H.store.DissolvePair(ctx, f.A, "new-unpair", request)
	require.ErrorIs(t, err, db.ErrConflict)
	require.NoError(t, f.H.store.RevokeUser(ctx, f.A.UserID, true))
	_, err = f.H.store.DissolvePair(ctx, f.A, "unpair", request)
	require.Error(t, err)
}

func TestPairDissolutionPreservesAssignedPassesDepositAndPendingEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	event, err := f.H.store.Event(ctx, admin.EventScope, f.EventID)
	require.NoError(t, err)
	event.Document.Deposit = &registrations.DepositPolicy{MinimumMinor: 3000, MinimumBPS: 2500}
	event, err = f.H.store.ChangeEvent(
		ctx,
		admin.EventScope,
		admin.Subject,
		"pair-deposit",
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document},
	)
	require.NoError(t, err)
	args.ExpectedEventVersion = event.Version
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "assign-pair", args)
	require.NoError(t, err)
	assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.NoError(t, err)
	require.NotNil(t, assigned.Leader.Pass.Deposit)
	require.NotNil(t, assigned.Follower.Pass.Deposit)
	sent := time.Now().UTC().Truncate(time.Second)
	file := paymentFile(t, f.H, f.A.UserID, 12011, "uncouple-deposit-proof", sent)
	evidence, err := f.H.store.SubmitPaymentEvidence(ctx, f.A, "deposit-proof", payments.Submission{
		EventID:       f.EventID,
		PassID:        assigned.Leader.Pass.ID,
		FileID:        file,
		AmountMinor:   3087,
		Currency:      "BYN",
		CurrencyScale: 2,
		Purpose:       payments.Initial,
	})
	require.NoError(t, err)
	review := payments.ReviewChange{
		EventID:     f.EventID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
	approval, err := f.H.store.PreparePaymentReview(ctx, admin, "confirm-deposit", review)
	require.NoError(t, err)
	_, err = f.H.store.ReviewPaymentEvidence(ctx, admin, "confirm-deposit", review, approval.ID)
	require.NoError(t, err)
	before, err := f.H.store.PaymentBalance(ctx, f.A, f.EventID, assigned.Leader.Pass.ID)
	require.NoError(t, err)
	require.True(t, before.DepositSatisfied)
	require.False(t, before.FullyPaid)
	pendingFile := paymentFile(t, f.H, f.B.UserID, 12012, "uncouple-pending-proof", sent)
	pending, err := f.H.store.SubmitPaymentEvidence(ctx, f.B, "pending-proof", payments.Submission{
		EventID:       f.EventID,
		PassID:        assigned.Follower.Pass.ID,
		FileID:        pendingFile,
		AmountMinor:   12345,
		Currency:      "BYN",
		CurrencyScale: 2,
		Purpose:       payments.Initial,
	})
	require.NoError(t, err)
	event.Document.Registration.ClosesAt = time.Now().UTC().Truncate(time.Microsecond).Add(-time.Second)
	_, err = f.H.store.ChangeEvent(
		ctx,
		admin.EventScope,
		admin.Subject,
		"close-sales",
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document},
	)
	require.NoError(t, err)
	result, err := f.H.store.DissolvePair(
		ctx,
		f.B,
		"paid-unpair",
		db.PairDissolution{EventID: f.EventID, PairID: args.PairID, ExpectedVersion: assigned.Pair.Version},
	)
	require.NoError(t, err)
	require.Equal(t, assigned.Pair.AssignedAt, result.AssignedAt)
	for _, original := range []registrations.Registration{assigned.Leader, assigned.Follower} {
		current, readErr := f.H.store.Registration(ctx, admin.EventScope, f.EventID, original.UserID)
		require.NoError(t, readErr)
		require.Equal(t, registrations.Assigned, current.State)
		require.Equal(t, original.Version+1, current.Version)
		require.Equal(t, original.Pass, current.Pass)
	}
	after, err := f.H.store.PaymentBalance(ctx, f.A, f.EventID, assigned.Leader.Pass.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	retained, err := f.H.store.PaymentEvidence(ctx, f.B, f.EventID, pending.ID)
	require.NoError(t, err)
	require.Equal(t, pending, retained)
	review = payments.ReviewChange{
		EventID:     f.EventID,
		EvidenceID:  pending.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 12345,
	}
	approval, err = f.H.store.PreparePaymentReview(ctx, admin, "confirm-after-unpair", review)
	require.NoError(t, err)
	_, err = f.H.store.ReviewPaymentEvidence(ctx, admin, "confirm-after-unpair", review, approval.ID)
	require.NoError(t, err)
	paid, err := f.H.store.PaymentBalance(ctx, f.B, f.EventID, assigned.Follower.Pass.ID)
	require.NoError(t, err)
	require.True(t, paid.FullyPaid)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.NoError(t, err)
}
