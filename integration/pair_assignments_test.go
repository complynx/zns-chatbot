package integration_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func acceptedPairForAssignment(t *testing.T) (pairFixture, db.PaymentScope, db.PairAssignment) {
	t.Helper()
	ctx := context.Background()
	f := setupPair(t)
	invitation := invitePair(t, f)
	accepted, err := f.H.store.DecidePairInvitation(ctx, f.B, "pair-accept", "accept", db.PairInvitationDecision{
		EventID: f.EventID, InvitationID: invitation.Invitation.ID, ExpectedVersion: 1, Role: registrations.Follower,
	})
	require.NoError(t, err)
	require.NotNil(t, accepted.Pair)
	leader, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
	require.NoError(t, err)
	follower, err := f.H.store.Registration(ctx, f.B.EventScope, f.EventID, f.B.UserID)
	require.NoError(t, err)
	adminID, _ := registrationUser(t, f.H, 12005, true)
	return f, paymentScope(t, f.H, adminID, true), db.PairAssignment{
		EventID:                 f.EventID,
		PairID:                  accepted.Pair.ID,
		ExpectedEventVersion:    1,
		ExpectedPairVersion:     accepted.Pair.Version,
		ExpectedLeaderVersion:   leader.Version,
		ExpectedFollowerVersion: follower.Version,
	}
}

func TestPairAssignmentCommitsBothPassesAndReplaysAfterRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	_, err := f.H.store.PreparePairAssignment(ctx, f.A, "not-admin", args)
	require.ErrorIs(t, err, db.ErrPairDenied)
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "assign-pair", args)
	require.NoError(t, err)
	repeated, err := f.H.store.PreparePairAssignment(ctx, admin, "assign-pair", args)
	require.NoError(t, err)
	require.Equal(t, prep, repeated)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, uuid.New())
	require.ErrorIs(t, err, db.ErrConflict)
	result, err := f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, "assigned", result.Pair.State)
	require.Equal(t, args.ExpectedPairVersion+1, result.Pair.Version)
	for _, entry := range []registrations.Registration{result.Leader, result.Follower} {
		require.Equal(t, registrations.Assigned, entry.State)
		require.NotNil(t, entry.Pass)
		require.Equal(t, int64(12345), entry.Pass.PriceMinor)
		require.Equal(t, "BYN", entry.Pass.Currency)
		require.Equal(t, *result.Pair.AssignedAt, entry.Pass.AssignedAt)
	}
	require.NotEqual(t, result.Leader.Pass.ID, result.Follower.Pass.ID)
	require.Equal(t, result.Leader.Pass.PaymentDueAt, result.Follower.Pass.PaymentDueAt)
	require.Equal(t, f.Entry.RequestedAt, result.Leader.RequestedAt)
	f.H.stop(t)
	f.H.start(t)
	_, err = f.H.store.Pool().
		Exec(ctx, "UPDATE shuttle_pair_preparations SET expires_at=clock_timestamp()-interv" +
"al '1 second' WHERE proposal_id=$1", prep.ID)
	require.NoError(t, err)
	replay, err := f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	changed := args
	changed.ExpectedPairVersion++
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", changed, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	require.NoError(t, f.H.store.RevokeUser(ctx, admin.UserID, true))
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.Error(t, err)
}

func TestPairAssignmentFullFollowerCapacityLeavesBothUnassigned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "assign-pair", args)
	require.NoError(t, err)
	otherUser, err := f.H.store.GetUser(ctx, f.Other.UserID)
	require.NoError(t, err)
	other, err := f.H.store.ChangeRegistration(
		ctx,
		f.Other.EventScope,
		otherUser.Subject,
		"other-follower",
		db.RegistrationChange{
			Kind: "register", EventID: f.EventID, UserID: f.Other.UserID, Role: registrations.Follower,
		},
	)
	require.NoError(t, err)
	_, err = f.H.store.ChangeRegistration(ctx, admin.EventScope, admin.Subject, "fill-follower", db.RegistrationChange{
		Kind:                 "assign",
		EventID:              f.EventID,
		RegistrationID:       other.ID,
		ExpectedEventVersion: 1,
		ExpectedVersion:      other.Version,
	})
	require.NoError(t, err)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.ErrorIs(t, err, registrations.ErrCapacity)
	for _, owner := range []db.PaymentScope{f.A, f.B} {
		entry, readErr := f.H.store.Registration(ctx, owner.EventScope, f.EventID, owner.UserID)
		require.NoError(t, readErr)
		require.Equal(t, registrations.Waitlisted, entry.State)
		require.Nil(t, entry.Pass)
	}
}

func TestPairAssignmentRollsBackFirstPassWhenSecondWriteFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f, admin, args := acceptedPairForAssignment(t)
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "assign-pair", args)
	require.NoError(t, err)
	follower, err := f.H.store.Registration(ctx, f.B.EventScope, f.EventID, f.B.UserID)
	require.NoError(t, err)
	// Fail the second pass insertion after the first pass has been written in its transaction.
	_, err = f.H.store.Pool().Exec(ctx, fmt.Sprintf(`
CREATE FUNCTION reject_pair_follower() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.registration_id='%s'::uuid THEN RAISE EXCEPTION 'synthetic second pass failure'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reject_pair_follower BEFORE INSERT ON shuttle_passes
FOR EACH ROW EXECUTE FUNCTION reject_pair_follower();`, follower.ID))
	require.NoError(t, err)
	_, err = f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.Error(t, err)
	for _, owner := range []db.PaymentScope{f.A, f.B} {
		entry, readErr := f.H.store.Registration(ctx, owner.EventScope, f.EventID, owner.UserID)
		require.NoError(t, readErr)
		require.Equal(t, registrations.Waitlisted, entry.State)
		require.Nil(t, entry.Pass)
	}
	_, err = f.H.store.Pool().
		Exec(ctx, "DROP TRIGGER reject_pair_follower ON shuttle_passes; DROP FUNCTION reject_pair_follower()")
	require.NoError(t, err)
	result, err := f.H.store.ConfirmPairAssignment(ctx, admin, "assign-pair", args, prep.ID)
	require.NoError(t, err)
	require.NotNil(t, result.Leader.Pass)
	require.NotNil(t, result.Follower.Pass)
}
