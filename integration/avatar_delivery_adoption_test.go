package integration_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func TestAvatarAdoptionSharesOneProposalAcrossConcurrentOperations(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	ctx := t.Context()
	original := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(ctx, scope, original, a)
	require.NoError(t, err)
	operations := []uuid.UUID{original, uuid.New(), uuid.New()}
	for _, op := range operations[1:] {
		adopted, adoptErr := f.h.store.AdoptAvatarDeliveryResolution(ctx, scope, op, prep.ID, a)
		require.NoError(t, adoptErr)
		require.Equal(t, prep, adopted)
		replay, replayErr := f.h.store.AdoptAvatarDeliveryResolution(ctx, scope, op, prep.ID, a)
		require.NoError(t, replayErr)
		require.Equal(t, prep, replay)
		view, readErr := f.h.store.AvatarDeliveryProposal(
			ctx,
			scope,
			db.AvatarDeliveryProposalSelector{OperationID: op},
		)
		require.NoError(t, readErr)
		require.Equal(t, original, view.OperationID)
		require.Equal(t, prep, view.AvatarDeliveryPreparation)
	}
	page, err := f.h.store.PendingAvatarDeliveryProposals(ctx, scope, uuid.Nil, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, original, page.Items[0].OperationID)
	var wg sync.WaitGroup
	results := make([]db.AvatarDeliveryResolutionResult, len(operations))
	errs := make([]error, len(operations))
	start := make(chan struct{})
	for i, op := range operations {
		wg.Go(func() { <-start; results[i], errs[i] = f.h.store.ResolveAvatarDelivery(ctx, scope, op, a, prep.ID) })
	}
	close(start)
	wg.Wait()
	for i, op := range operations {
		require.NoError(t, errs[i])
		require.Equal(t, results[0], results[i])
		receipt, readErr := f.h.store.AvatarDeliveryResolutionReceipt(ctx, scope, op)
		require.NoError(t, readErr)
		require.Equal(t, results[0], receipt)
	}
	current, err := f.h.store.AvatarDelivery(ctx, avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n.Version+1, current.Version)
	require.Equal(t, "pending", current.State)
	require.Equal(t, n.Receipt, results[0].PreviousReceipt)
	page, err = f.h.store.PendingAvatarDeliveryProposals(ctx, scope, uuid.Nil, 0)
	require.NoError(t, err)
	require.Empty(t, page.Items)
}

func TestAvatarAdoptionRejectsChangedArgumentsAndActor(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	op := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, op, a)
	require.NoError(t, err)
	alias := uuid.New()
	changed := a
	changed.Reason = "different authorization"
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, alias, prep.ID, changed)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, alias, uuid.Nil, a)
	require.ErrorIs(t, err, db.ErrInvalidAvatar)
	other := scope
	other.ProfileScope = f.scope
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), other, alias, prep.ID, a)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	other = scope
	other.Organization = "another-org"
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), other, alias, prep.ID, a)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	other = scope
	other.GlobalAdmin = false
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), other, alias, prep.ID, a)
	require.ErrorIs(t, err, payments.ErrDenied)
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, alias, prep.ID, a)
	require.NoError(t, err)
	competing, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, uuid.New(), a)
	require.NoError(t, err)
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, alias, competing.ID, a)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, op, competing.ID, a)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.h.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, alias, prep.ID, a)
	require.Error(t, err)
}

func TestAvatarAdoptionNeverRenewsExpiredOrCompletedConsent(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	original := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, original, a)
	require.NoError(t, err)
	alias := uuid.New()
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, alias, prep.ID, a)
	require.NoError(t, err)
	result, err := f.h.store.ResolveAvatarDelivery(t.Context(), scope, alias, a, prep.ID)
	require.NoError(t, err)
	for _, op := range []uuid.UUID{alias, uuid.New()} {
		_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, op, prep.ID, a)
		require.ErrorIs(t, err, db.ErrConflict)
	}
	receipt, err := f.h.store.AvatarDeliveryResolutionReceipt(t.Context(), scope, original)
	require.NoError(t, err)
	require.Equal(t, result, receipt)
	// Use a separate still-repairable delivery to exercise expiry without completion.
	g, admin, _, args := avatarRepairFixture(t)
	expiredOp := uuid.New()
	expired, err := g.h.store.PrepareAvatarDeliveryResolution(t.Context(), admin, expiredOp, args)
	require.NoError(t, err)
	expiredAlias := uuid.New()
	_, err = g.h.store.AdoptAvatarDeliveryResolution(t.Context(), admin, expiredAlias, expired.ID, args)
	require.NoError(t, err)
	_, err = g.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_avatar_delivery_resolutions"+
			" SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1", expired.ID)
	require.NoError(t, err)
	for _, op := range []uuid.UUID{expiredAlias, uuid.New()} {
		_, err = g.h.store.AdoptAvatarDeliveryResolution(t.Context(), admin, op, expired.ID, args)
		require.ErrorIs(t, err, db.ErrConflict)
	}
	_, err = g.h.store.ResolveAvatarDelivery(t.Context(), admin, expiredAlias, args, expired.ID)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestAvatarAdoptionPreservesOriginalHistory(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	original := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, original, a)
	require.NoError(t, err)
	var preserved []byte
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "SELECT document FROM shuttle_avatar_delivery_resolutions WHERE proposal_id=$1", prep.ID).
			Scan(&preserved),
	)
	var old db.AvatarDeliveryPreparation
	require.NoError(t, json.Unmarshal(preserved, &old))
	require.Equal(t, prep, old)
	alias := uuid.New()
	_, err = f.h.store.AdoptAvatarDeliveryResolution(t.Context(), scope, alias, prep.ID, a)
	require.NoError(t, err)
	result, err := f.h.store.ResolveAvatarDelivery(t.Context(), scope, alias, a, prep.ID)
	require.NoError(t, err)
	replay, err := f.h.store.AvatarDeliveryResolutionReceipt(t.Context(), scope, original)
	require.NoError(t, err)
	require.Equal(t, result, replay)
}

func TestAvatarAdoptionAPIUsesOriginalProposalAndNormalizesControl(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	token := avatarAdminToken(f, scope)
	original := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, original, a)
	require.NoError(t, err)
	alias := uuid.New()
	arguments := map[string]any{
		"delivery_id":        a.DeliveryID,
		"expected_version":   a.ExpectedVersion,
		"action":             a.Action,
		"reason":             a.Reason,
		"resume_proposal_id": prep.ID,
	}
	request := map[string]any{"protocol_version": 1, "operation_id": alias, "arguments": arguments}
	code, raw := adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI+"/prepare", request)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared struct {
		Arguments     map[string]any `json:"arguments"`
		Preconditions map[string]any `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.NotContains(t, prepared.Arguments, "resume_proposal_id")
	require.Equal(t, prep.ID.String(), prepared.Preconditions["proposal_id"])
	request["arguments"] = prepared.Arguments
	request["idempotency_key"] = alias
	request["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prepared.Preconditions}
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, request)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"succeeded"`)
	for _, op := range []uuid.UUID{original, alias} {
		code, raw = adminRequest(
			t,
			f.h,
			token,
			http.MethodPost,
			avatarResolutionAPI+"/status",
			map[string]any{"protocol_version": 1, "operation_id": op},
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Contains(t, string(raw), `"status":"succeeded"`)
	}
	request["arguments"] = arguments
	prepareAgain := map[string]any{"protocol_version": 1, "operation_id": alias, "arguments": arguments}
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI+"/prepare", prepareAgain)
	require.Equal(t, http.StatusConflict, code, string(raw))
	arguments["resume_proposal_id"] = uuid.New()
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, request)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
}
