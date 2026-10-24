package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func scopedAccount(t *testing.T, h *harness, botID, telegramID int64, subject string) db.User {
	t.Helper()
	task, _, err := h.store.Intake(context.Background(), db.IntakeRequest{
		BotID: botID, UpdateID: telegramID, TelegramUserID: telegramID, ChatID: telegramID,
		Profile: json.RawMessage(`{"first_name":"foreign-profile-canary"}`), Payload: json.RawMessage(`{}`),
	}, nil)
	require.NoError(t, err)
	user, err := h.store.BindSubject(context.Background(), task.UserID, subject, "org")
	require.NoError(t, err)
	return user
}

func TestAPIAccountIdentityIsScopedToCurrentBot(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submit(t, 1)
	h.await(t, id, "completed")
	task, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	owner, err := h.store.GetUser(context.Background(), task.UserID)
	require.NoError(t, err)
	foreign := scopedAccount(t, h, 1, 789, owner.Subject)
	token := p.token(owner.Subject)
	body := map[string]any{"protocol_version": 1, "operation_id": "account-scope", "arguments": map[string]any{}}
	read := func() (int, []byte) {
		return adminRequest(t, h, token, http.MethodPost, "/internal/agent/account/me", body)
	}
	code, data := read()
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), owner.ID.String())
	require.NotContains(t, string(data), "foreign-profile-canary")
	code, data = adminRequest(t, h, token, http.MethodGet, "/internal/tasks/"+id.String(), nil)
	require.Equal(t, http.StatusOK, code, string(data))
	_, err = h.store.SetUserRevoked(context.Background(), foreign.ID, "fixture")
	require.NoError(t, err)
	code, data = read()
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, h.store.RevokeUser(context.Background(), foreign.ID, false))
	_, err = h.store.SetUserRevoked(context.Background(), owner.ID, "fixture")
	require.NoError(t, err)
	code, _ = read()
	require.Equal(t, http.StatusForbidden, code)
	onlyForeign := scopedAccount(t, h, 1, 790, "foreign-only-subject")
	code, _ = adminRequest(t, h, p.token(onlyForeign.Subject), http.MethodPost, "/internal/agent/account/me", body)
	require.Equal(t, http.StatusForbidden, code)
}

func TestAdminRevocationCannotCrossBotBoundary(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submit(t, 1)
	h.await(t, id, "completed")
	token := taskAdminToken(t, h, id)
	foreign := scopedAccount(t, h, 124, 790, "foreign-revocation-target")
	code, data := adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/admin/users/"+foreign.ID.String()+"/revoke",
		map[string]any{},
	)
	require.Equal(t, http.StatusNotFound, code, string(data))
	unchanged, err := h.store.GetUser(context.Background(), foreign.ID)
	require.NoError(t, err)
	require.False(t, unchanged.Revoked)
	local := scopedAccount(t, h, 123, 791, "local-revocation-target")
	code, data = adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/admin/users/"+local.ID.String()+"/revoke",
		map[string]any{},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	changed, err := h.store.GetUser(context.Background(), local.ID)
	require.NoError(t, err)
	require.True(t, changed.Revoked)
}
