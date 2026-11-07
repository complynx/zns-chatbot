package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestPartnerCodeReadIsOwnerOnlyAndNeverInventsUnverifiedURL(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	token := pairUserToken(f, f.B)
	code, raw := invitationAPIRequest(t, f, token, "partner_code", "own-code", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var response struct {
		Result map[string]any `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Len(t, response.Result, 2)
	require.Equal(t, f.B.UserID.String(), response.Result["partner_code"])
	require.Contains(t, response.Result["share_url"], "https://t.me/zinusya_test_bot?start=partner_")
	code, _ = invitationAPIRequest(
		t,
		f,
		token,
		"partner_code",
		"target-code",
		map[string]any{"target_user_id": f.A.UserID},
	)
	require.Equal(t, http.StatusBadRequest, code)
	for _, invalid := range []*telegram.User{
		{ID: 999, IsBot: true, Username: "other_test_bot"}, {ID: 123, Username: "other_test_bot"},
		{ID: 123, IsBot: true, Username: "bad?injection"},
	} {
		f.H.peers.mu.Lock()
		f.H.peers.getMeUser = invalid
		f.H.peers.mu.Unlock()
		code, raw = invitationAPIRequest(t, f, token, "partner_code", "invalid-metadata", map[string]any{})
		require.Equal(t, http.StatusOK, code, string(raw))
		require.NotContains(t, string(raw), "share_url")
		require.Contains(t, string(raw), f.B.UserID.String())
	}
	f.H.peers.mu.Lock()
	f.H.peers.getMeFailure = true
	f.H.peers.mu.Unlock()
	code, raw = invitationAPIRequest(t, f, token, "partner_code", "offline-telegram", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.NotContains(t, string(raw), "share_url")
	require.Contains(t, string(raw), f.B.UserID.String())
	require.NoError(t, f.H.store.RevokeUser(context.Background(), f.B.UserID, true))
	code, _ = invitationAPIRequest(t, f, token, "partner_code", "revoked-code", map[string]any{})
	require.Equal(t, http.StatusForbidden, code)
}

func TestPartnerCodeRechecksRevocationAfterTelegramLookup(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	token := pairUserToken(f, f.B)
	entered, release := make(chan struct{}), make(chan struct{})
	f.H.peers.mu.Lock()
	f.H.peers.getMeEntered, f.H.peers.getMeRelease = entered, release
	f.H.peers.mu.Unlock()
	result := make(chan int, 1)
	request := httptest.NewRequest(http.MethodPost, "/internal/agent/pairs/partner_code", strings.NewReader(
		`{"protocol_version":1,"operation_id":"blocked-lookup","arguments":{}}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	go func() {
		response := httptest.NewRecorder()
		f.H.endpoint.InternalHandler().ServeHTTP(response, request)
		result <- response.Code
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("Telegram lookup did not start")
	}
	err := f.H.store.RevokeUser(context.Background(), f.B.UserID, true)
	close(release)
	require.NoError(t, err)
	select {
	case code := <-result:
		require.Equal(t, http.StatusNotFound, code)
	case <-time.After(5 * time.Second):
		t.Fatal("Partner code read did not finish")
	}
}
