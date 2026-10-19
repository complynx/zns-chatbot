package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestOrderEvidenceReceiptLookupPreservesAdmissionAcrossReviewAndRestart(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61201, "read-order-receipt")
	op := uuid.New()
	admitted, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	reviewOp, _, confirmed := reviewOrderEvidence(t, f, admitted, db.OrderEvidenceConfirmed)
	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	owner := paymentScope(t, f.H, f.OwnerID, false)
	receipt, err := reopened.OrderEvidenceOperationByID(t.Context(), owner, op, db.OrderEvidenceSubmissionReceipt)
	require.NoError(t, err)
	require.Equal(t, op, receipt.OperationID)
	require.Equal(t, admitted, receipt.Result, "The original admission must remain pending")
	current, err := reopened.OwnOrderEvidence(t.Context(), scope, order.EventID, admitted.ID)
	require.NoError(t, err)
	require.Equal(t, confirmed, current)
	actor := paymentScope(t, f.H, f.AmbassadorID, false)
	review, err := reopened.OrderEvidenceOperationByID(t.Context(), actor, reviewOp, db.OrderEvidenceReviewReceipt)
	require.NoError(t, err)
	require.Equal(t, confirmed, review.Result)
	_, err = reopened.OrderEvidenceOperationByID(t.Context(), owner, reviewOp, db.OrderEvidenceReviewReceipt)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Knowing an operation UUID never grants another actor's receipt")
	_, err = reopened.OrderEvidenceOperationByID(t.Context(), actor, reviewOp, db.OrderEvidenceSubmissionReceipt)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Phase must match")
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	_, err = reopened.OrderEvidenceOperationByID(t.Context(), actor, reviewOp, db.OrderEvidenceReviewReceipt)
	require.Error(t, err, "Role loss blocks historical review receipt recovery")
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	_, err = reopened.OrderEvidenceOperationByID(t.Context(), owner, op, db.OrderEvidenceSubmissionReceipt)
	require.Error(t, err, "Revocation blocks historical submission receipt recovery")
}

func TestOrderEvidenceProofAccessIsPurposeScopedAndOrdersOnly(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61202, "read-order-file")
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['orders'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	owner := paymentScope(t, f.H, f.OwnerID, false)
	actor := paymentScope(t, f.H, f.AmbassadorID, false)
	own, err := f.H.store.OrderEvidenceFile(t.Context(), owner, evidence.ID, false)
	require.NoError(t, err)
	require.Equal(t, args.FileID, own.ID)
	proof, err := f.H.store.OrderEvidenceFile(t.Context(), actor, evidence.ID, true)
	require.NoError(t, err)
	require.Equal(t, own, proof)
	_, err = f.H.store.OrderEvidenceFile(t.Context(), actor, evidence.ID, false)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Owner route does not borrow reviewer authority")
	_, err = f.H.store.OrderEvidenceFile(t.Context(), owner, evidence.ID, true)
	require.Error(t, err, "Proof ownership is not event authority")
	_, err = f.H.store.OrderEvidenceFile(t.Context(), actor, args.FileID, true)
	require.ErrorIs(t, err, pgx.ErrNoRows, "Foreign file IDs are not evidence locators")
	current, err := f.H.store.OrderEvidenceForReview(t.Context(), actor, f.Event.ID, evidence.ID)
	require.NoError(t, err)
	require.Equal(t, evidence, current)
	_, err = f.H.store.OrderEvidenceForReview(t.Context(), actor, uuid.New(), evidence.ID)
	require.Error(t, err)
	foreign := actor
	foreign.Organization = "other"
	_, err = f.H.store.OrderEvidenceFile(t.Context(), foreign, evidence.ID, true)
	require.Error(t, err)
	foreign = actor
	foreign.BotID++
	_, err = f.H.store.OrderEvidenceFile(t.Context(), foreign, evidence.ID, true)
	require.Error(t, err)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.OwnerID)
	require.NoError(t, err)
	_, err = f.H.store.OrderEvidenceFile(t.Context(), owner, evidence.ID, false)
	require.Error(t, err)
	proof, err = f.H.store.OrderEvidenceFile(t.Context(), actor, evidence.ID, true)
	require.NoError(t, err, "Payer revocation must not prevent authority from inspecting historical proof")
	require.Equal(t, own, proof)
	current, err = f.H.store.OrderEvidenceForReview(t.Context(), actor, f.Event.ID, evidence.ID)
	require.NoError(t, err)
	require.Equal(t, evidence, current)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	_, err = f.H.store.OrderEvidenceFile(t.Context(), actor, evidence.ID, true)
	require.Error(t, err)
	_, err = f.H.store.OrderEvidenceForReview(t.Context(), actor, f.Event.ID, evidence.ID)
	require.Error(t, err)
}

func TestOrderEvidenceReadAndRecoveryRequireCurrentModule(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	args := orderEvidenceArgs(t, f, order, 61203, "read-order-module")
	op := uuid.New()
	evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, op, args)
	require.NoError(t, err)
	owner := paymentScope(t, f.H, f.OwnerID, false)
	actor := paymentScope(t, f.H, f.AmbassadorID, false)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=ARRAY['registrations'],version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.OrderEvidenceOperationByID(t.Context(), owner, op, db.OrderEvidenceSubmissionReceipt)
	require.Error(t, err)
	_, err = f.H.store.OrderEvidenceFile(t.Context(), owner, evidence.ID, false)
	require.Error(t, err)
	_, err = f.H.store.OrderEvidenceFile(t.Context(), actor, evidence.ID, true)
	require.Error(t, err)
	_, err = f.H.store.OrderEvidenceForReview(t.Context(), actor, f.Event.ID, evidence.ID)
	require.Error(t, err)
}
