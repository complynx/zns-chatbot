package integration_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func avatarRepairFixture(
	t *testing.T,
) (avatarDraftFixture, db.AvatarDeliveryAdminScope, db.AvatarDelivery, db.AvatarDeliveryResolution) {
	t.Helper()
	f, receipt, _, _ := queuedAvatar(t)
	id, _ := registrationUser(t, f.h, 999, true)
	scope := db.AvatarDeliveryAdminScope{
		ProfileScope: ownerProfileScope(t, f.h, id),
		GlobalAdmin:  true,
	}
	n, _, err := f.h.store.ReserveAvatarDelivery(t.Context(), f.scope, receipt.DeliveryID, 1, uuid.New())
	require.NoError(t, err)
	require.NoError(
		t,
		f.h.store.FinishAvatarDelivery(
			t.Context(),
			n,
			"delivery_unknown",
			json.RawMessage(`{"error":"receipt lost"}`),
			0,
		),
	)
	n, err = f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
	require.NoError(t, err)
	return f, scope, n, db.AvatarDeliveryResolution{
		DeliveryID:      n.ID,
		ExpectedVersion: n.Version,
		Action:          "retry",
		Reason:          "checked chat; explicit last resort",
	}
}
func TestAvatarResolutionExplicitRetryKeepsAuditAndDelivers(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	ctx := t.Context()
	op := uuid.New()
	proposal, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, op, a)
	require.NoError(t, err)
	require.Contains(t, proposal.Description, "duplicate")
	require.Equal(t, n, proposal.Delivery)
	unchanged, err := f.h.store.AvatarDelivery(ctx, avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, unchanged)
	result, err := f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, proposal.ID)
	require.NoError(t, err)
	require.Equal(t, n.Receipt, result.PreviousReceipt)
	require.Equal(t, n.AttemptID, result.PreviousAttemptID)
	require.Equal(t, n.Version, result.PreviousVersion)
	require.Equal(t, "pending", result.State)
	require.Equal(t, n.Version+1, result.Version)
	f.h.start(t)
	awaitDraftAvatarDelivery(t, f, n.ID, "delivered")
	attempts, sent := avatarDocumentCounts(f)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, sent)
	replay, err := f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, proposal.ID)
	require.NoError(t, err)
	require.Equal(t, result, replay)
	read, err := f.h.store.AvatarDeliveryResolutionReceipt(ctx, scope, op)
	require.NoError(t, err)
	require.Equal(t, result, read)
}
func TestAvatarResolutionDismissesRevokedOwnerAndFencesOldAttempt(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	ctx := t.Context()
	a.Action = "dismiss"
	_, err := f.h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.scope.UserID)
	require.NoError(t, err)
	retry := a
	retry.Action = "retry"
	_, err = f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, uuid.New(), retry)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	op := uuid.New()
	proposal, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, op, a)
	require.NoError(t, err)
	result, err := f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, proposal.ID)
	require.NoError(t, err)
	require.Equal(t, "dismissed", result.State)
	require.ErrorIs(t, f.h.store.FinishAvatarDelivery(ctx, n, "delivered", json.RawMessage(`{}`), 0), db.ErrConflict)
	// Exact replay does not depend on current recipient identity or proposal time.
	_, err = f.h.store.Pool().
		Exec(ctx, "UPDATE shuttle_avatar_delivery_resolutions"+
			" SET expires_at=clock_timestamp()-interval '1 hour' WHERE operation_id=$1", op)
	require.NoError(t, err)
	replay, err := f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, proposal.ID)
	require.NoError(t, err)
	require.Equal(t, result, replay)
}
func TestAvatarResolutionRequiresCurrentAdminAndDeployment(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	ctx := t.Context()
	op := uuid.New()
	denied := scope
	denied.GlobalAdmin = false
	_, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, denied, op, a)
	require.ErrorIs(t, err, payments.ErrDenied)
	foreign := scope
	foreign.Organization = "another-org"
	_, err = f.h.store.PrepareAvatarDeliveryResolution(ctx, foreign, op, a)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	proposal, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, op, a)
	require.NoError(t, err)
	changed := a
	changed.Reason = "different reason"
	_, err = f.h.store.ResolveAvatarDelivery(ctx, scope, op, changed, proposal.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.h.store.ResolveAvatarDelivery(ctx, denied, op, a, proposal.ID)
	require.ErrorIs(t, err, payments.ErrDenied)
	_, err = f.h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, proposal.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.h.store.AvatarDeliveryResolutionReceipt(ctx, scope, op)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
func TestAvatarResolutionRejectsExpiryStaleCASAndLiveAttempt(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	ctx := t.Context()
	op := uuid.New()
	proposal, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, op, a)
	require.NoError(t, err)
	_, err = f.h.store.Pool().
		Exec(ctx, "UPDATE shuttle_avatar_delivery_resolutions"+
			" SET expires_at=clock_timestamp()-interval '1 second' WHERE operation_id=$1", op)
	require.NoError(t, err)
	_, err = f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, proposal.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	second := uuid.New()
	p2, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, second, a)
	require.NoError(t, err)
	_, err = f.h.store.Pool().
		Exec(ctx, "UPDATE shuttle_avatar_deliveries SET version=version+1,state='send_inflight' WHERE id=$1", n.ID)
	require.NoError(t, err)
	_, err = f.h.store.ResolveAvatarDelivery(ctx, scope, second, a, p2.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	a.ExpectedVersion++
	_, err = f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, uuid.New(), a)
	require.ErrorIs(t, err, db.ErrConflict)
}
func TestAvatarResolutionConcurrentConfirmHasOneEffect(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	ctx := t.Context()
	op := uuid.New()
	p, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, op, a)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make([]db.AvatarDeliveryResolutionResult, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() { results[i], errs[i] = f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, p.ID) })
	}
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.Equal(t, results[0], results[1])
	require.Equal(t, n.Version+1, results[0].Version)
	current, err := f.h.store.AvatarDelivery(ctx, avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n.Version+1, current.Version)
	require.Equal(t, uuid.Nil, current.AttemptID)
}
func TestAvatarResolutionRechecksChangedRecipientAtConfirmation(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	ctx := t.Context()
	op := uuid.New()
	p, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, op, a)
	require.NoError(t, err)
	_, err = f.h.store.Pool().Exec(ctx, "UPDATE shuttle_users SET tg_user_id=tg_user_id+1 WHERE id=$1", n.UserID)
	require.NoError(t, err)
	_, err = f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, p.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	current, err := f.h.store.AvatarDelivery(ctx, avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, current)
}

func TestAvatarResolutionRetainsPreparedAndCompletedHistory(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	ctx := t.Context()
	op := uuid.New()
	p, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, op, a)
	require.NoError(t, err)
	result, err := f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, p.ID)
	require.NoError(t, err)
	receipt, err := f.h.store.AvatarDeliveryResolutionReceipt(ctx, scope, op)
	require.NoError(t, err)
	require.Equal(t, result, receipt)
}
