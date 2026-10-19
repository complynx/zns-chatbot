package integration_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func TestAvatarProposalRecoversOriginalAndCompletedReceipt(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	op := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, op, a)
	require.NoError(t, err)
	for _, selector := range []db.AvatarDeliveryProposalSelector{{OperationID: op}, {ProposalID: prep.ID}} {
		view, readErr := f.h.store.AvatarDeliveryProposal(t.Context(), scope, selector)
		require.NoError(t, readErr)
		require.Equal(t, prep, view.AvatarDeliveryPreparation)
		require.Equal(t, op, view.OperationID)
		require.Equal(t, "pending", view.State)
		require.Nil(t, view.Result)
	}
	_, err = f.h.store.AvatarDeliveryResolutionReceipt(t.Context(), scope, op)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	result, err := f.h.store.ResolveAvatarDelivery(t.Context(), scope, op, a, prep.ID)
	require.NoError(t, err)
	_, err = f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_avatar_delivery_resolutions SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1", prep.ID)
	require.NoError(t, err)
	view, err := f.h.store.AvatarDeliveryProposal(
		t.Context(),
		scope,
		db.AvatarDeliveryProposalSelector{ProposalID: prep.ID},
	)
	require.NoError(t, err)
	require.Equal(t, "completed", view.State)
	require.Equal(t, prep.Delivery, view.Delivery)
	require.Equal(t, result, *view.Result)
	page, err := f.h.store.PendingAvatarDeliveryProposals(t.Context(), scope, uuid.Nil, 0)
	require.NoError(t, err)
	require.Empty(t, page.Items)
}

func TestAvatarProposalChecksCurrentActorAndSelectors(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	op := uuid.New()
	prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, op, a)
	require.NoError(t, err)
	for _, selector := range []db.AvatarDeliveryProposalSelector{{}, {OperationID: op, ProposalID: prep.ID}} {
		_, err = f.h.store.AvatarDeliveryProposal(t.Context(), scope, selector)
		require.ErrorIs(t, err, db.ErrInvalidAvatar)
	}
	other := scope
	other.ProfileScope = f.scope
	for _, selector := range []db.AvatarDeliveryProposalSelector{{OperationID: op}, {ProposalID: prep.ID}} {
		_, err = f.h.store.AvatarDeliveryProposal(t.Context(), other, selector)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	page, err := f.h.store.PendingAvatarDeliveryProposals(t.Context(), other, uuid.Nil, 0)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	scope.GlobalAdmin = false
	_, err = f.h.store.AvatarDeliveryProposal(t.Context(), scope, db.AvatarDeliveryProposalSelector{OperationID: op})
	require.ErrorIs(t, err, payments.ErrDenied)
	scope.GlobalAdmin = true
	_, err = f.h.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	_, err = f.h.store.AvatarDeliveryProposal(t.Context(), scope, db.AvatarDeliveryProposalSelector{OperationID: op})
	require.Error(t, err)
	_, err = f.h.store.PendingAvatarDeliveryProposals(t.Context(), scope, uuid.Nil, 0)
	require.Error(t, err)
}

func TestAvatarProposalPendingPagesBoundBytesAndExcludeExpiry(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	expected := map[uuid.UUID]bool{}
	for i := range 6 {
		prep, err := f.h.store.PrepareAvatarDeliveryResolution(t.Context(), scope, uuid.New(), a)
		require.NoError(t, err)
		if i == 0 {
			_, err = f.h.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_avatar_delivery_resolutions SET expires_at=clock_timestamp()-interval '1 second' WHERE proposal_id=$1", prep.ID)
			require.NoError(t, err)
			view, readErr := f.h.store.AvatarDeliveryProposal(
				t.Context(),
				scope,
				db.AvatarDeliveryProposalSelector{ProposalID: prep.ID},
			)
			require.NoError(t, readErr)
			require.Equal(t, "expired", view.State)
			continue
		}
		expected[prep.ID] = true
		// Stored proposals are individually bounded; a page must also bound their aggregate.
		_, err = f.h.store.Pool().
			Exec(t.Context(), "UPDATE shuttle_avatar_delivery_resolutions SET document=jsonb_set(document,'{description}',to_jsonb($2::text)) WHERE proposal_id=$1", prep.ID, strings.Repeat("x", 14000))
		require.NoError(t, err)
	}
	after := uuid.Nil
	pages := 0
	for {
		page, err := f.h.store.PendingAvatarDeliveryProposals(t.Context(), scope, after, 20)
		require.NoError(t, err)
		raw, err := json.Marshal(page)
		require.NoError(t, err)
		require.Less(t, len(raw), 64*1024)
		require.NotEmpty(t, page.Items)
		for _, item := range page.Items {
			require.True(t, expected[item.ID], "missing or repeated proposal")
			delete(expected, item.ID)
		}
		pages++
		require.LessOrEqual(t, pages, 5)
		if page.NextCursor == nil {
			break
		}
		require.Equal(t, page.Items[len(page.Items)-1].ID, *page.NextCursor)
		after = *page.NextCursor
	}
	require.Empty(t, expected)
	require.Greater(t, pages, 1)
	for _, limit := range []int{-1, 21} {
		_, err := f.h.store.PendingAvatarDeliveryProposals(t.Context(), scope, uuid.Nil, limit)
		require.ErrorIs(t, err, db.ErrInvalidAvatar)
	}
}

func TestAvatarProposalAPIRecoveryAndAuthorization(t *testing.T) {
	t.Parallel()
	f, scope, _, a := avatarRepairFixture(t)
	token := avatarAdminToken(f, scope)
	op := uuid.New()
	body := preparedAvatarRepair(t, f, token, op, a)
	pre := body["confirmation"].(map[string]any)["preconditions"].(map[string]any)
	proposal := pre["proposal_id"]
	read := func(token, path string, args map[string]any) (int, []byte) {
		return adminRequest(
			t,
			f.h,
			token,
			http.MethodPost,
			path,
			map[string]any{"protocol_version": 1, "operation_id": uuid.NewString(), "arguments": args},
		)
	}
	proposalPath := "/internal/agent/administration/avatar_delivery_proposal"
	pendingPath := "/internal/agent/administration/avatar_delivery_pending"
	code, raw := read(token, proposalPath, map[string]any{"proposal_id": proposal})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), op.String())
	require.Contains(t, string(raw), `"state":"pending"`)
	code, raw = read(token, pendingPath, map[string]any{"limit": 1})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), op.String())
	for _, args := range []map[string]any{{}, {"operation_id": op, "proposal_id": proposal}} {
		code, raw = read(token, proposalPath, args)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	f.p.mu.Lock()
	f.p.adminRole = false
	ordinary := f.p.token(f.scope.Subject)
	f.p.adminRole = true
	f.p.mu.Unlock()
	for _, path := range []string{proposalPath, pendingPath} {
		args := map[string]any{}
		if path == proposalPath {
			args["proposal_id"] = proposal
		}
		code, raw = read(ordinary, path, args)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	code, raw = adminRequest(
		t,
		f.h,
		token,
		http.MethodPost,
		avatarResolutionAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": op},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
}
