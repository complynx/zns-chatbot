package integration_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
)

const appTestOrigin = "https://app.example.invalid"
const appBootstrapPath = "/app/session/bootstrap"

type appSession struct {
	Token     string    `json:"session_token"`
	UserID    uuid.UUID `json:"user_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

func appProof(userID int64) string {
	user := `{"id":` + strconv.FormatInt(userID, 10) + `,"first_name":"Mini App user","language_code":"ru"}`
	date := strconv.FormatInt(time.Now().Unix(), 10)
	key := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = key.Write([]byte("123:test-token"))
	mac := hmac.New(sha256.New, key.Sum(nil))
	_, _ = mac.Write([]byte("auth_date=" + date + "\nuser=" + user))
	return url.Values{"auth_date": {date}, "user": {user}, "hash": {hex.EncodeToString(mac.Sum(nil))}}.Encode()
}

func appBody(proof string, seed byte) string {
	raw, _ := json.Marshal(
		map[string]string{
			"init_data":       proof,
			"bootstrap_nonce": base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32)),
		},
	)
	return string(raw)
}

func appCall(t *testing.T, h *harness, method, path, body, token, origin string) (int, []byte, http.Header) {
	t.Helper()
	code, raw, headers, err := appHTTPCall(h, method, path, body, token, origin)
	require.NoError(t, err)
	return code, raw, headers
}

func appHTTPCall(h *harness, method, path, body, token, origin string) (int, []byte, http.Header, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, h.server.URL+path, strings.NewReader(body))
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if token != "" {
		req.Header.Set("Authorization", "ZinusyaSession "+token)
	}
	response, err := h.server.Client().Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<16))
	return response.StatusCode, raw, response.Header, err
}

func TestMiniAppBootstrapRetryRestartAndLogoutDoNotStartAgentWork(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	body := appBody(appProof(456), 1)
	code, raw, headers := appCall(t, h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	require.NotContains(t, string(raw), "access_token")
	require.NotContains(t, string(raw), "test-token")
	require.NotContains(t, string(raw), "init_data")
	var first appSession
	require.NoError(t, json.Unmarshal(raw, &first))
	require.NotEmpty(t, first.Token)
	require.NotEqual(t, uuid.Nil, first.UserID)
	code, retry, _ := appCall(t, h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(retry))
	require.JSONEq(t, string(raw), string(retry), "Lost reply retries preserve the exact credential and expiry")
	for _, table := range []string{"shuttle_tasks", "shuttle_conversations"} {
		var count int
		require.NoError(t, h.store.Pool().QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
	h.stop(t)
	h.start(t)
	code, read, _ := appCall(t, h, http.MethodGet, "/app/me", "", first.Token, "")
	require.Equal(t, http.StatusOK, code, string(read))
	require.NotContains(t, string(read), "session_token")
	var recovered appSession
	require.NoError(t, json.Unmarshal(read, &recovered))
	require.Equal(t, first.UserID, recovered.UserID)
	require.Equal(t, first.ExpiresAt, recovered.ExpiresAt)
	for _, scheme := range []string{"ZinusyaSession", "Bearer"} {
		internal := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/internal/agent/account/me",
			strings.NewReader(`{"protocol_version":1,"operation_id":"op","arguments":{}}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", scheme+" "+first.Token)
		h.endpoint.InternalHandler().ServeHTTP(internal, request)
		require.Equal(
			t,
			http.StatusUnauthorized,
			internal.Code,
			"Application capability does not authorize agent tools",
		)
	}
	p.mu.Lock()
	sentCount, runCount := len(p.sent), len(p.runs)
	p.mu.Unlock()
	require.Zero(t, sentCount)
	require.Zero(t, runCount)
	code, _, _ = appCall(t, h, http.MethodPost, "/app/session/logout", "{}", first.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code)
	code, _, _ = appCall(t, h, http.MethodGet, "/app/me", "", first.Token, "")
	require.Equal(t, http.StatusUnauthorized, code)
	code, _, _ = appCall(t, h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code, "Consumed proof cannot recreate a logged-out session")
}

func TestMiniAppUnknownIAMCreateRecoversSamePendingIdentity(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.loseUserCreateAndStopIAM = true
	h := setup(t, p)
	body := appBody(appProof(777), 2)
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	var pendingID, userID uuid.UUID
	var expiry time.Time
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(context.Background(), "SELECT id,user_id,expires_at FROM shuttle_miniapp_sessions WHERE state='pending'").
			Scan(&pendingID, &userID, &expiry),
	)
	p.mu.Lock()
	userCount := len(p.users)
	_, exists := p.users[userID.String()]
	p.iamDown = false
	p.mu.Unlock()
	require.Equal(t, 1, userCount)
	require.True(t, exists, "IAM proposed ID is the already committed local user UUID")
	h.stop(t)
	h.start(t)
	code, raw, _ = appCall(t, h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var active appSession
	require.NoError(t, json.Unmarshal(raw, &active))
	require.Equal(t, userID, active.UserID)
	require.True(t, expiry.Equal(active.ExpiresAt), "Original absolute expiry survives response encoding and restart")
	var activeID uuid.UUID
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(context.Background(), "SELECT id FROM shuttle_miniapp_sessions WHERE state='active'").
			Scan(&activeID),
	)
	require.Equal(t, pendingID, activeID)
	p.mu.Lock()
	userCount, sentCount, runCount := len(p.users), len(p.sent), len(p.runs)
	p.mu.Unlock()
	require.Equal(t, 1, userCount)
	require.Zero(t, sentCount)
	require.Zero(t, runCount)
}

func TestMiniAppRejectsForeignOriginReplayAndRevocationDespiteCachedIAM(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	require.NoError(t, h.iam.SetPositiveCacheTTL(time.Minute))
	proof := appProof(888)
	body := appBody(proof, 3)
	for _, origin := range []string{"", "null", "https://foreign.example.invalid"} {
		code, _, headers := appCall(t, h, http.MethodPost, appBootstrapPath, body, "", origin)
		require.Equal(t, http.StatusForbidden, code)
		require.Empty(t, headers.Get("Access-Control-Allow-Origin"))
	}
	duplicate := strings.TrimSuffix(body, "}") + `,"bootstrap_nonce":"other"}`
	code, _, _ := appCall(t, h, http.MethodPost, appBootstrapPath, duplicate, "", appTestOrigin)
	require.Equal(t, http.StatusBadRequest, code)
	code, _, _ = appCall(
		t,
		h,
		http.MethodPost,
		appBootstrapPath,
		appBody(strings.Replace(proof, "888", "889", 1), 3),
		"",
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnauthorized, code)
	var count int
	require.NoError(
		t,
		h.store.Pool().QueryRow(context.Background(), "SELECT count(*) FROM shuttle_miniapp_sessions").Scan(&count),
	)
	require.Zero(t, count, "Invalid requests do not create durable login attempts")
	code, raw, _ := appCall(t, h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var session appSession
	require.NoError(t, json.Unmarshal(raw, &session))
	rotated, err := api.New(h.store, h.iam, h.service, api.Options{
		BotID:           123,
		WebhookSecret:   "test-secret",
		OrganizationID:  "org",
		ActorID:         "actor",
		Issuer:          p.iam.URL,
		LoomAudience:    "loom",
		MiniAppOrigin:   appTestOrigin,
		MiniAppBotToken: "123:rotated-token",
	})
	require.NoError(t, err)
	rotatedResponse := httptest.NewRecorder()
	rotatedRequest := httptest.NewRequest(http.MethodGet, "/app/me", nil)
	rotatedRequest.Header.Set("Authorization", "ZinusyaSession "+session.Token)
	rotated.PublicHandler().ServeHTTP(rotatedResponse, rotatedRequest)
	require.Equal(
		t,
		http.StatusUnauthorized,
		rotatedResponse.Code,
		"Current bot key generation is required even for a stored bearer hash",
	)
	code, _, _ = appCall(t, h, http.MethodPost, appBootstrapPath, appBody(proof, 4), "", appTestOrigin)
	require.Equal(t, http.StatusConflict, code)
	p.mu.Lock()
	p.iamDown = true
	p.mu.Unlock()
	code, _, _ = appCall(t, h, http.MethodGet, "/app/me", "", session.Token, "")
	require.Equal(t, http.StatusOK, code, "Still-valid IAM cache hides a brief outage")
	require.NoError(t, h.store.RevokeUser(context.Background(), session.UserID, true))
	code, _, _ = appCall(t, h, http.MethodGet, "/app/me", "", session.Token, "")
	require.Equal(t, http.StatusUnauthorized, code, "Local revocation bypasses neither session nor IAM cache")
	code, _, _ = appCall(t, h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestMiniAppAndTelegramFirstLoginShareOneIAMAccount(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	type bootstrapResult struct {
		Code int
		Body []byte
		Err  error
	}
	result := make(chan bootstrapResult, 1)
	body := appBody(appProof(456), 5)
	go func() {
		defer close(result)
		code, raw, _, err := appHTTPCall(h, http.MethodPost, appBootstrapPath, body, "", appTestOrigin)
		result <- bootstrapResult{Code: code, Body: raw, Err: err}
	}()
	taskID := h.submitText(t, 2001, "Hello from the Telegram client")
	boot, ok := <-result
	require.True(t, ok)
	require.NoError(t, boot.Err)
	require.Equal(t, http.StatusOK, boot.Code, string(boot.Body))
	var session appSession
	require.NoError(t, json.Unmarshal(boot.Body, &session))
	h.await(t, taskID, "completed")
	accepted, err := h.store.GetTask(context.Background(), taskID)
	require.NoError(t, err)
	require.Equal(t, session.UserID, accepted.UserID)
	p.mu.Lock()
	users := len(p.users)
	p.mu.Unlock()
	require.Equal(t, 1, users, "Simultaneous bot/web creation uses one persisted identity UUID")
}
