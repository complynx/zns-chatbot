package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const avatarResolutionAPI = "/internal/agent/administration/resolve_avatar_delivery"
const avatarInspectionAPI = "/internal/agent/administration/avatar_delivery"

func avatarAdminToken(f avatarDraftFixture, scope db.AvatarDeliveryAdminScope) string {
	f.p.mu.Lock()
	defer f.p.mu.Unlock()
	return f.p.token(scope.Subject)
}
func avatarResolutionRequest(op uuid.UUID, a db.AvatarDeliveryResolution) map[string]any {
	return map[string]any{"protocol_version": 1, "operation_id": op.String(), "arguments": a}
}

func preparedAvatarRepair(
	t *testing.T,
	f avatarDraftFixture,
	token string,
	op uuid.UUID,
	a db.AvatarDeliveryResolution,
) map[string]any {
	t.Helper()
	body := avatarResolutionRequest(op, a)
	code, raw := adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep struct {
		Status        string                      `json:"status"`
		Description   string                      `json:"description"`
		Arguments     db.AvatarDeliveryResolution `json:"arguments"`
		Preconditions map[string]any              `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, "prepared", prep.Status)
	require.Equal(t, a, prep.Arguments)
	if a.Action == "retry" {
		require.Contains(t, prep.Description, "duplicate")
	}
	body["idempotency_key"] = op.String()
	body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
	return body
}
func TestAvatarResolutionAPIConfirmsReconcilesAndDelivers(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	token := avatarAdminToken(f, scope)
	op := uuid.New()
	code, raw := adminRequest(
		t,
		f.h,
		token,
		http.MethodPost,
		avatarInspectionAPI,
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.NewString(),
			"arguments":        map[string]any{"delivery_id": n.ID},
		},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "receipt lost")
	require.Contains(t, string(raw), n.ArtifactID.String())
	body := preparedAvatarRepair(t, f, token, op, a)
	statusBody := map[string]any{"protocol_version": 1, "operation_id": op.String()}
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI+"/status", statusBody)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var resolved struct {
		Status string                            `json:"status"`
		Result db.AvatarDeliveryResolutionResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &resolved))
	require.Equal(t, "succeeded", resolved.Status, string(raw))
	require.Equal(t, n.AttemptID, resolved.Result.PreviousAttemptID)
	f.h.start(t)
	awaitDraftAvatarDelivery(t, f, n.ID, "delivered")
	for _, suffix := range []string{"/status", "/reconcile", ""} {
		request := statusBody
		if suffix == "" {
			request = body
		}
		code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI+suffix, request)
		require.Equal(t, http.StatusOK, code, string(raw))
		var replay struct {
			Status string                            `json:"status"`
			Result db.AvatarDeliveryResolutionResult `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &replay))
		require.Equal(t, "succeeded", replay.Status, string(raw))
		require.Equal(t, resolved.Result, replay.Result)
	}
	attempts, sent := avatarDocumentCounts(f)
	require.Equal(t, 1, attempts)
	require.Equal(t, 1, sent)
	code, raw = adminRequest(t, f.h, token, http.MethodPost, "/internal/agent/administration/discover", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"id":"resolve_avatar_delivery"`)
	require.Contains(t, string(raw), `"id":"avatar_delivery"`)
}
func TestAvatarResolutionAPIRejectsOrdinaryAndRevokedAdministrators(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	admin := avatarAdminToken(f, scope)
	f.p.mu.Lock()
	f.p.adminRole = false
	ordinary := f.p.token(f.scope.Subject)
	f.p.adminRole = true
	f.p.mu.Unlock()
	op := uuid.New()
	confirmed := preparedAvatarRepair(t, f, admin, op, a)
	statusBody := map[string]any{"protocol_version": 1, "operation_id": op.String()}
	readBody := map[string]any{
		"protocol_version": 1,
		"operation_id":     op.String(),
		"arguments":        map[string]any{"delivery_id": n.ID},
	}
	for _, request := range []struct {
		path string
		body map[string]any
	}{
		{avatarResolutionAPI + "/prepare", avatarResolutionRequest(op, a)},
		{avatarResolutionAPI, confirmed},
		{avatarResolutionAPI + "/status", statusBody},
		{avatarResolutionAPI + "/reconcile", statusBody},
		{avatarInspectionAPI, readBody},
	} {
		code, raw := adminRequest(t, f.h, ordinary, http.MethodPost, request.path, request.body)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	code, raw := adminRequest(t, f.h, ordinary, http.MethodPost, "/internal/agent/administration/discover", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), "resolve_avatar_delivery")
	_, err := f.h.store.Pool().Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", scope.UserID)
	require.NoError(t, err)
	code, raw = adminRequest(t, f.h, admin, http.MethodPost, avatarResolutionAPI, confirmed)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	code, raw = adminRequest(
		t,
		f.h,
		admin,
		http.MethodPost,
		avatarResolutionAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": op.String()},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	current, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, current)
}
func TestAvatarResolutionAPIRequiresExactUnexpiredProposal(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	token := avatarAdminToken(f, scope)
	op := uuid.New()
	body := preparedAvatarRepair(t, f, token, op, a)
	confirmation := body["confirmation"]
	delete(body, "confirmation")
	code, raw := adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmation"] = confirmation
	body["idempotency_key"] = uuid.NewString()
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["idempotency_key"] = op.String()
	changed := a
	changed.Action = "dismiss"
	body["arguments"] = changed
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	body["arguments"] = a
	_, err := f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_avatar_delivery_resolutions SET expires_at=clock_timestam" +
"p()-interval '1 second' WHERE operation_id=$1", op)
	require.NoError(t, err)
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	current, err := f.h.store.AvatarDelivery(t.Context(), avatarDeployment(f), n.ID)
	require.NoError(t, err)
	require.Equal(t, n, current)
}
func TestAvatarResolutionAPIScopesInspectionAndRechecksIAM(t *testing.T) {
	t.Parallel()
	f, scope, n, a := avatarRepairFixture(t)
	token := avatarAdminToken(f, scope)
	op := uuid.New()
	body := preparedAvatarRepair(t, f, token, op, a)
	_, err := f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_users SET zitadel_org='other-org' WHERE id=$1", n.UserID)
	require.NoError(t, err)
	code, raw := adminRequest(
		t,
		f.h,
		token,
		http.MethodPost,
		avatarInspectionAPI,
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.NewString(),
			"arguments":        map[string]any{"delivery_id": n.ID},
		},
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = adminRequest(t, f.h, token, http.MethodPost, avatarResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	require.NoError(t, f.h.iam.SetPositiveCacheTTL(0))
	f.p.mu.Lock()
	f.p.userInactive = true
	f.p.mu.Unlock()
	code, raw = adminRequest(
		t,
		f.h,
		token,
		http.MethodPost,
		avatarResolutionAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": op.String()},
	)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}
