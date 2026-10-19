package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func pairAssignmentInAnotherEvent(t *testing.T, f pairFixture, admin db.PaymentScope) db.PairAssignment {
	t.Helper()
	ctx := t.Context()
	event := registrationEvent(t, f.H, pairUserToken(f, admin))
	leader := registerForEvent(t, f.H, pairUserToken(f, f.A), event.ID, "second-event-join", registrations.Leader)
	invited, err := f.H.store.CreatePairInvitation(ctx, f.A, "second-event-invite", db.PairInvitationRequest{
		EventID: event.ID, TargetUserID: f.B.UserID, ExpectedVersion: leader.Version,
	})
	require.NoError(t, err)
	accepted, err := f.H.store.DecidePairInvitation(
		ctx,
		f.B,
		"second-event-accept",
		"accept",
		db.PairInvitationDecision{
			EventID: event.ID, InvitationID: invited.Invitation.ID, ExpectedVersion: 1, Role: registrations.Follower,
		},
	)
	require.NoError(t, err)
	require.NotNil(t, accepted.Pair)
	leader, err = f.H.store.Registration(ctx, f.A.EventScope, event.ID, f.A.UserID)
	require.NoError(t, err)
	follower, err := f.H.store.Registration(ctx, f.B.EventScope, event.ID, f.B.UserID)
	require.NoError(t, err)
	return db.PairAssignment{
		EventID: event.ID, PairID: accepted.Pair.ID, ExpectedEventVersion: event.Version,
		ExpectedPairVersion: accepted.Pair.Version, ExpectedLeaderVersion: leader.Version,
		ExpectedFollowerVersion: follower.Version,
	}
}

func TestPairAssignmentProposalsPaginateOwnedCanonicalDocumentsWithinCurrentEventAuthority(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	otherArgs := pairAssignmentInAnotherEvent(t, f, admin)
	first, err := f.H.store.PreparePairAssignment(ctx, admin, "first", args)
	require.NoError(t, err)
	second, err := f.H.store.PreparePairAssignment(ctx, admin, "second", args)
	require.NoError(t, err)
	other, err := f.H.store.PreparePairAssignment(ctx, admin, "other-event", otherArgs)
	require.NoError(t, err)
	_, err = f.H.store.AdoptPairAssignment(ctx, admin, "first-alias", args, first.ID)
	require.NoError(t, err)
	var ids []uuid.UUID
	after := uuid.Nil
	for range 3 {
		page, e := f.H.store.PendingPairAssignmentProposals(ctx, admin, after, 1)
		require.NoError(t, e)
		require.Len(t, page.Proposals, 1)
		view := page.Proposals[0]
		require.Equal(t, db.AssignmentProposalPending, view.State)
		require.Nil(t, view.Result)
		require.False(t, view.ObservedAt.IsZero())
		ids = append(ids, view.Preparation.ID)
		if len(ids) < 3 {
			require.NotNil(t, page.Next)
			after = *page.Next
		} else {
			require.Nil(t, page.Next)
		}
	}
	require.ElementsMatch(t, []uuid.UUID{first.ID, second.ID, other.ID}, ids)
	foreignID, _ := registrationUser(t, f.H, 12902, true)
	foreign := paymentScope(t, f.H, foreignID, true)
	_, err = f.H.store.PairAssignmentProposal(ctx, foreign, first.ID)
	require.Error(t, err)
	page, err := f.H.store.PendingPairAssignmentProposals(ctx, foreign, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, page.Proposals)
	_, err = f.H.store.PairAssignmentProposal(ctx, f.A, first.ID)
	require.Error(t, err)

	authority, err := f.H.store.ChangeAuthority(ctx, admin.EventScope, admin.Subject, "grant-owned-event",
		db.AuthorityChange{EventID: args.EventID, UserID: admin.UserID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, err)
	scoped := admin
	scoped.GlobalAdmin = false
	page, err = f.H.store.PendingPairAssignmentProposals(ctx, scoped, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, page.Proposals, 2)
	for _, view := range page.Proposals {
		require.Equal(t, args.EventID, view.Preparation.Arguments.EventID)
	}
	view, err := f.H.store.PairAssignmentProposal(ctx, scoped, first.ID)
	require.NoError(t, err)
	require.Equal(t, first, view.Preparation)
	require.Equal(t, "first", view.OriginalOperationID)
	_, err = f.H.store.PairAssignmentProposal(ctx, scoped, other.ID)
	require.Error(t, err)
	_, err = f.H.store.AdoptPairAssignment(ctx, scoped, "outside-authority", otherArgs, other.ID)
	require.Error(t, err)
	_, err = f.H.store.ConfirmPairAssignment(ctx, scoped, "other-event", otherArgs, other.ID)
	require.Error(t, err)

	_, err = f.H.store.ChangeAuthority(
		ctx,
		admin.EventScope,
		admin.Subject,
		"downgrade-owned-event",
		db.AuthorityChange{
			EventID:         args.EventID,
			UserID:          admin.UserID,
			Role:            db.AuthorityAmbassador,
			ExpectedVersion: authority.Version,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	_, err = f.H.store.PairAssignmentProposal(ctx, scoped, first.ID)
	require.Error(t, err)
	page, err = f.H.store.PendingPairAssignmentProposals(ctx, scoped, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, page.Proposals)
	_, err = f.H.store.AdoptPairAssignment(ctx, scoped, "after-role-loss", args, first.ID)
	require.Error(t, err)
	_, err = f.H.store.ConfirmPairAssignment(ctx, scoped, "first-alias", args, first.ID)
	require.Error(t, err)

	// Global authority still reads its own documents, but cannot substitute another identity.
	_, err = f.H.store.PairAssignmentProposal(ctx, admin, first.ID)
	require.NoError(t, err)
	spoofed := admin
	spoofed.UserID = foreign.UserID
	_, err = f.H.store.PairAssignmentProposal(ctx, spoofed, first.ID)
	require.Error(t, err)
	page, err = f.H.store.PendingPairAssignmentProposals(ctx, spoofed, uuid.Nil, 20)
	require.ErrorIs(t, err, db.ErrPairDenied)
	require.Empty(t, page.Proposals)
	require.NoError(t, f.H.store.RevokeUser(ctx, admin.UserID, true))
	_, err = f.H.store.PairAssignmentProposal(ctx, admin, first.ID)
	require.Error(t, err)
	page, err = f.H.store.PendingPairAssignmentProposals(ctx, admin, uuid.Nil, 20)
	require.ErrorIs(t, err, db.ErrPairDenied)
	require.Empty(t, page.Proposals)
	_, err = f.H.store.AdoptPairAssignment(ctx, admin, "revoked-actor", args, first.ID)
	require.Error(t, err)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "first-alias", args, first.ID)
	require.Error(t, err)
}

func TestPairAssignmentPendingProposalsExcludeExpiredAndCompletedDocuments(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	expired, err := f.H.store.PreparePairAssignment(ctx, admin, "expired", args)
	require.NoError(t, err)
	completed, err := f.H.store.PreparePairAssignment(ctx, admin, "completed", args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_pair_preparations SET expires_at=clock_timestamp()-interval '1 minute' WHERE proposal_id=$1", expired.ID)
	require.NoError(t, err)
	page, err := f.H.store.PendingPairAssignmentProposals(ctx, admin, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, page.Proposals, 1)
	require.Equal(t, completed.ID, page.Proposals[0].Preparation.ID)
	assigned, err := f.H.store.ConfirmPairAssignment(ctx, admin, "completed", args, completed.ID)
	require.NoError(t, err)
	page, err = f.H.store.PendingPairAssignmentProposals(ctx, admin, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, page.Proposals)
	require.Nil(t, page.Next)
	view, err := f.H.store.PairAssignmentProposal(ctx, admin, expired.ID)
	require.NoError(t, err)
	require.Equal(t, db.AssignmentProposalExpired, view.State)
	require.Nil(t, view.Result)
	view, err = f.H.store.PairAssignmentProposal(ctx, admin, completed.ID)
	require.NoError(t, err)
	require.Equal(t, db.AssignmentProposalCompleted, view.State)
	require.NotNil(t, view.Result)
	require.Equal(t, assigned, *view.Result)
}
