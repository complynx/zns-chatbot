package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestPairAssignmentAdoptionSharesApprovedTermsAndOneReceiptAcrossRestart(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	original := "native-pair-original"
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, original, args)
	require.NoError(t, err)
	require.Equal(t, "Europe/Minsk", prep.Timezone)
	require.Len(t, prep.Quotes, 2)
	operations := []string{original, uuid.NewString(), uuid.NewString()}
	for _, operation := range operations[1:] {
		adopted, e := f.H.store.AdoptPairAssignment(ctx, admin, operation, args, prep.ID)
		require.NoError(t, e)
		require.Equal(t, prep, adopted, "Adoption must preserve the complete approved document")
		repeated, e := f.H.store.PreparePairAssignment(ctx, admin, operation, args)
		require.NoError(t, e)
		require.Equal(t, prep, repeated)
	}
	f.H.stop(t)
	f.H.start(t)
	reopened, err := db.Open(ctx, f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	t.Cleanup(reopened.Close)
	type outcome struct {
		result db.PairAssignmentResult
		err    error
	}
	finished := make(chan outcome, len(operations))
	start := make(chan struct{})
	for i, operation := range operations {
		store := f.H.store
		if i%2 == 1 {
			store = reopened
		}
		go func() {
			<-start
			result, e := store.ConfirmPairAssignment(ctx, admin, operation, args, prep.ID)
			finished <- outcome{result, e}
		}()
	}
	close(start)
	var assigned db.PairAssignmentResult
	for range operations {
		select {
		case received := <-finished:
			require.NoError(t, received.err)
			require.NotNil(t, received.result.Leader.Pass)
			require.NotNil(t, received.result.Follower.Pass)
			if assigned.Leader.Pass == nil {
				assigned = received.result
			} else {
				require.Equal(t, assigned, received.result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent pair confirmations did not finish")
		}
	}
	require.NotEqual(t, assigned.Leader.Pass.ID, assigned.Follower.Pass.ID)
	for _, operation := range operations {
		receipt, e := reopened.PairAssignmentOperation(ctx, admin, operation)
		require.NoError(t, e)
		require.Equal(t, assigned, receipt)
	}
	var passes, receipts int
	require.NoError(t, reopened.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Equal(t, 2, passes)
	require.NoError(t, reopened.Pool().QueryRow(ctx,
		"SELECT count(*) FROM shuttle_pair_operations WHERE actor=$1 AND kind='assign'", admin.Subject).Scan(&receipts))
	require.Equal(t, 1, receipts)

	_, err = reopened.Pool().Exec(ctx,
		"UPDATE shuttle_pair_preparations SET expires_at=clock_timestamp()-interval '1 minute' WHERE proposal_id=$1", prep.ID)
	require.NoError(t, err)
	_, err = reopened.Pool().Exec(ctx,
		"UPDATE shuttle_event_registration_rules SET document=jsonb_set(document," +
"'{closes_at}',to_jsonb(clock_timestamp()-interval '1 minute')) WHERE eve" +
"nt_id=$1", f.EventID)
	require.NoError(t, err)
	view, err := reopened.PairAssignmentProposal(ctx, admin, prep.ID)
	require.NoError(t, err)
	require.Equal(t, db.AssignmentProposalCompleted, view.State)
	require.Equal(t, original, view.OriginalOperationID)
	require.Equal(t, prep, view.Preparation)
	require.NotNil(t, view.Result)
	require.Equal(t, assigned, *view.Result)
	require.False(t, view.ObservedAt.IsZero())
	for _, operation := range operations {
		replay, e := reopened.ConfirmPairAssignment(ctx, admin, operation, args, prep.ID)
		require.NoError(t, e)
		require.Equal(t, assigned, replay)
		_, e = reopened.AdoptPairAssignment(ctx, admin, operation, args, prep.ID)
		require.ErrorIs(t, e, db.ErrConflict)
	}
	_, err = reopened.AdoptPairAssignment(ctx, admin, "new-after-completion", args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = reopened.PreparePairAssignment(ctx, admin, "new-effect", args)
	require.Error(t, err)
	changed := args
	changed.ExpectedFollowerVersion++
	_, err = reopened.ConfirmPairAssignment(ctx, admin, operations[1], changed, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestPairAssignmentAdoptionRejectsChangedForeignExpiredAndOccupiedOperations(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, admin, args := acceptedPairForAssignment(t)
	prep, err := f.H.store.PreparePairAssignment(ctx, admin, "original", args)
	require.NoError(t, err)
	foreignID, _ := registrationUser(t, f.H, 12901, true)
	foreign := paymentScope(t, f.H, foreignID, true)
	_, err = f.H.store.AdoptPairAssignment(ctx, foreign, "foreign", args, prep.ID)
	require.Error(t, err)
	changed := args
	changed.ExpectedLeaderVersion++
	_, err = f.H.store.AdoptPairAssignment(ctx, admin, "changed", changed, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.AdoptPairAssignment(ctx, admin, "missing", args, uuid.New())
	require.Error(t, err)
	other, err := f.H.store.PreparePairAssignment(ctx, admin, "other-canonical", args)
	require.NoError(t, err)
	_, err = f.H.store.AdoptPairAssignment(ctx, admin, "other-canonical", args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.AdoptPairAssignment(ctx, admin, "alias", args, prep.ID)
	require.NoError(t, err)
	_, err = f.H.store.AdoptPairAssignment(ctx, admin, "alias", args, other.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	for _, kind := range []string{"invite", "accept", "decline", "revoke", "dissolve", "assign"} {
		operation := "occupied-" + kind
		// Historical receipts reserve their transport key regardless of receipt kind.
		_, err = f.H.store.Pool().
			Exec(ctx, "INSERT INTO shuttle_pair_operations(bot_id,organization_id,actor,operati" +
"on_id,kind,request_hash,result)\n SELECT bot_id,organization_id,actor,$1," +
"$2,request_hash,'{}'::jsonb FROM shuttle_pair_preparations WHERE proposa" +
"l_id=$3", operation, kind, prep.ID)
		require.NoError(t, err)
		_, err = f.H.store.AdoptPairAssignment(ctx, admin, operation, args, prep.ID)
		require.ErrorIs(t, err, db.ErrConflict, kind)
	}
	_, err = f.H.store.Pool().Exec(ctx,
		"UPDATE shuttle_pair_preparations SET expires_at=clock_timestamp()-interval '1 minute' WHERE proposal_id=$1", prep.ID)
	require.NoError(t, err)
	for _, operation := range []string{"expired-new", "alias"} {
		_, err = f.H.store.AdoptPairAssignment(ctx, admin, operation, args, prep.ID)
		require.ErrorIs(t, err, db.ErrConflict)
		_, err = f.H.store.ConfirmPairAssignment(ctx, admin, operation, args, prep.ID)
		require.Error(t, err)
	}
	view, err := f.H.store.PairAssignmentProposal(ctx, admin, prep.ID)
	require.NoError(t, err)
	require.Equal(t, db.AssignmentProposalExpired, view.State)
	require.Nil(t, view.Result)
	var passes int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_passes").Scan(&passes))
	require.Zero(t, passes)
}

func TestPairAssignmentReservedOperationsPreserveExactParticipantReceipts(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	f, global, args := acceptedPairForAssignment(t)
	_, err := f.H.store.ChangeAuthority(ctx, global.EventScope, global.Subject, "grant-participant-admin",
		db.AuthorityChange{EventID: f.EventID, UserID: f.A.UserID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, err)
	prep, err := f.H.store.PreparePairAssignment(ctx, f.A, "canonical", args)
	require.NoError(t, err)
	_, err = f.H.store.AdoptPairAssignment(ctx, f.A, "alias", args, prep.ID)
	require.NoError(t, err)
	request := db.PairInvitationRequest{EventID: f.EventID, TargetUserID: f.B.UserID, ExpectedVersion: f.Entry.Version}
	before, err := f.H.store.CreatePairInvitation(ctx, f.A, "pair-invite", request)
	require.NoError(t, err)
	_, err = f.H.store.AdoptPairAssignment(ctx, f.A, "pair-invite", args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	for _, operation := range []string{"canonical", "alias"} {
		_, err = f.H.store.CreatePairInvitation(ctx, f.A, operation, request)
		require.ErrorIs(t, err, db.ErrConflict)
		// The previous binary's write shape must also respect reserved aliases.
		_, err = f.H.store.Pool().
			Exec(ctx, "INSERT INTO shuttle_pair_operations(bot_id,organization_id,actor,operati" +
"on_id,kind,request_hash,result)\n SELECT bot_id,organization_id,actor,$1," +
"'invite',decode(repeat('00',32),'hex'),'{}'::jsonb FROM shuttle_pair_pre" +
"parations WHERE proposal_id=$2", operation, prep.ID)
		require.Error(t, err)
	}
	_, err = f.H.store.Pool().
		Exec(ctx, "INSERT INTO shuttle_pair_preparations(bot_id,organization_id,actor,opera" +
"tion_id,proposal_id,request_hash,expires_at,document)\n SELECT bot_id,org" +
"anization_id,actor,'alias',$1,request_hash,expires_at,document FROM shut" +
"tle_pair_preparations WHERE proposal_id=$2", uuid.New(), prep.ID)
	require.Error(t, err)
	after, err := f.H.store.CreatePairInvitation(ctx, f.A, "pair-invite", request)
	require.NoError(t, err)
	require.Equal(t, before, after)
	request.ExpectedVersion++
	_, err = f.H.store.CreatePairInvitation(ctx, f.A, "pair-invite", request)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestPairAssignmentAdoptionChecksExpiryAfterWaitingForProposal(t *testing.T) {
	t.Parallel()
	f, admin, args := acceptedPairForAssignment(t)
	prep, err := f.H.store.PreparePairAssignment(t.Context(), admin, "original", args)
	require.NoError(t, err)
	writer, err := f.H.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = writer.Rollback(t.Context()) }()
	_, err = writer.Exec(
		t.Context(),
		"SELECT proposal_id FROM shuttle_pair_preparations WHERE proposal_id=$1 FOR UPDATE",
		prep.ID,
	)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		_, e := f.H.store.AdoptPairAssignment(ctx, admin, "after-wait", args, prep.ID)
		finished <- e
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		e := f.H.store.Pool().QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_bl" +
"ocking_pids(pid)))", writer.Conn().PgConn().PID()).
			Scan(&blocked)
		return e == nil && blocked
	}, 2*time.Second, 10*time.Millisecond)
	_, err = writer.Exec(
		ctx,
		"UPDATE shuttle_pair_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1",
		prep.ID,
	)
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))
	select {
	case err = <-finished:
		require.ErrorIs(t, err, db.ErrConflict)
	case <-ctx.Done():
		t.Fatal("adoption did not finish")
	}
}
