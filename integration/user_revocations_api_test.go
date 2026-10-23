package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

const userRevocationAPI = "/internal/agent/administration/revoke_user"

func prepareRevocationBody(t *testing.T, h *harness, token, op string, args db.UserRevocationArguments) map[string]any {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args}
	code, raw := adminRequest(t, h, token, http.MethodPost, userRevocationAPI+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep struct {
		Status        string                     `json:"status"`
		Arguments     db.UserRevocationArguments `json:"arguments"`
		Preconditions map[string]any             `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, "prepared", prep.Status)
	require.Equal(t, args, prep.Arguments)
	body["idempotency_key"] = op
	body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
	return body
}

func readRevocationResult(t *testing.T, raw []byte) db.UserRevocationResult {
	t.Helper()
	var result struct {
		Status string                  `json:"status"`
		Result db.UserRevocationResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status, string(raw))
	return result.Result
}

func TestAgentUserRevocationRequiresRoleAndExactApprovalAndRecoversAfterRestart(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	h.start(t)
	h.stop(t)
	args := revocationArguments(target)
	code, raw := adminRequest(t, h, h.peers.token(scope.Subject), http.MethodPost, userRevocationAPI+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": "revoke", "arguments": args})
	require.Equal(t, http.StatusForbidden, code, string(raw))
	operator := settingsAdminToken(h, scope)
	code, raw = adminRequest(t, h, operator, http.MethodPost, "/internal/agent/administration/user",
		map[string]any{"protocol_version": 1, "operation_id": "lookup", "arguments": map[string]any{"id": target.ID}})
	require.Equal(t, http.StatusOK, code, string(raw))
	var lookup struct {
		Result db.UserRevocationArguments `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &lookup))
	require.Equal(t, args, lookup.Result)
	body := prepareRevocationBody(t, h, operator, "revoke", args)
	confirmation := body["confirmation"]
	delete(body, "confirmation")
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["confirmation"] = confirmation
	body["idempotency_key"] = "other"
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	body["idempotency_key"] = "revoke"
	body["confirmation"] = map[string]any{
		"mode":          "exact_parameters",
		"preconditions": map[string]any{"proposal_id": uuid.New()},
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	require.Zero(t, revocationAuditCount(t, h, target.ID))
	body["confirmation"] = confirmation
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	first := readRevocationResult(t, raw)
	require.Equal(t, target.ID, first.UserID)
	h.start(t)
	h.stop(t)
	for _, suffix := range []string{"status", "reconcile"} {
		code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI+"/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": "revoke"})
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, first, readRevocationResult(t, raw))
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, first, readRevocationResult(t, raw))
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
}

func TestAgentUserRevocationLostSelfCommitReplyRecoveredByAnotherAdmin(t *testing.T) {
	t.Parallel()
	h, scope, observer := revocationFixture(t)
	proxy := newCommitReplyProxy(t, h.store.Pool().Config().ConnString())
	store, err := db.Open(t.Context(), proxy.dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	h.store = store
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	self, err := h.store.GetUser(t.Context(), scope.UserID)
	require.NoError(t, err)
	body := prepareRevocationBody(t, h, operator, "self", revocationArguments(self))
	proxy.armed.Store(true)
	code, raw := adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	select {
	case <-proxy.dropped:
	case <-time.After(time.Second):
		t.Fatal("self-revocation COMMIT reply not dropped")
	}
	code, raw = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		userRevocationAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": "self"},
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	other := scope
	other.UserID, other.Subject = observer.ID, observer.Subject
	observerToken := settingsAdminToken(h, other)
	confirmation := body["confirmation"].(map[string]any)
	preconditions := confirmation["preconditions"].(map[string]any)
	code, raw = adminRequest(
		t,
		h,
		observerToken,
		http.MethodPost,
		"/internal/agent/administration/user_revocation_receipt",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "diagnose",
			"arguments":        map[string]any{"proposal_id": preconditions["proposal_id"]},
		},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var diagnostic struct {
		Result db.UserRevocationDiagnostic `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &diagnostic))
	require.Equal(t, scope.Subject, diagnostic.Result.Actor)
	require.Equal(t, "self", diagnostic.Result.OperationID)
	require.Equal(t, self.ID, diagnostic.Result.Result.UserID)
	require.True(t, diagnostic.Result.Result.Revoked)
	code, raw = adminRequest(t, h, observerToken, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	require.EqualValues(t, 1, revocationAuditCount(t, h, self.ID))
}

func TestAgentUserRevocationReplayInvalidatesOriginalAndCurrentScopedSubjects(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	settings, err := h.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.AuthCacheTTLSeconds = 120
	code, raw := adminRequest(t, h, operator, http.MethodPut, "/internal/admin/settings", settings)
	require.Equal(t, http.StatusOK, code, string(raw))
	body := prepareRevocationBody(t, h, operator, "revoke", revocationArguments(target))
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	original := readRevocationResult(t, raw)
	_, err = h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_users SET zitadel_subject='rebound-subject' WHERE id=$1", target.ID)
	require.NoError(t, err)
	credentials := make([]identity.Credential, 0, 2)
	for _, subject := range []string{target.Subject, "rebound-subject"} {
		credential, e := h.iam.UserToken(t.Context(), subject)
		require.NoError(t, e)
		_, e = h.iam.Verify(t.Context(), credential.Token)
		require.NoError(t, e)
		credentials = append(credentials, credential)
	}
	h.peers.mu.Lock()
	h.peers.iamDown = true
	h.peers.mu.Unlock()
	code, raw = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		userRevocationAPI+"/status",
		map[string]any{"protocol_version": 1, "operation_id": "revoke"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, original, readRevocationResult(t, raw))
	for i, subject := range []string{target.Subject, "rebound-subject"} {
		_, err = h.iam.UserToken(t.Context(), subject)
		require.ErrorIs(t, err, identity.ErrUnavailable)
		_, err = h.iam.Verify(t.Context(), credentials[i].Token)
		require.ErrorIs(t, err, identity.ErrUnavailable)
	}
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
}

func TestAgentUserRevocationLostCommitReplyInvalidatesCachesAndReconcilesOnce(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	proxy := newCommitReplyProxy(t, h.store.Pool().Config().ConnString())
	store, err := db.Open(t.Context(), proxy.dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	h.store = store
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	settings, err := h.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.AuthCacheTTLSeconds = 120
	code, raw := adminRequest(t, h, operator, http.MethodPut, "/internal/admin/settings", settings)
	require.Equal(t, http.StatusOK, code, string(raw))
	body := prepareRevocationBody(t, h, operator, "lost", revocationArguments(target))
	credential, err := h.iam.UserToken(t.Context(), target.Subject)
	require.NoError(t, err)
	_, err = h.iam.Verify(t.Context(), credential.Token)
	require.NoError(t, err)
	h.peers.mu.Lock()
	h.peers.iamDown = true
	h.peers.mu.Unlock()
	proxy.armed.Store(true)
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	select {
	case <-proxy.dropped:
	case <-time.After(time.Second):
		t.Fatal("revocation COMMIT reply not dropped")
	}
	_, err = h.iam.UserToken(t.Context(), target.Subject)
	require.ErrorIs(t, err, identity.ErrUnavailable)
	_, err = h.iam.Verify(t.Context(), credential.Token)
	require.ErrorIs(t, err, identity.ErrUnavailable)
	code, raw = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		userRevocationAPI+"/reconcile",
		map[string]any{"protocol_version": 1, "operation_id": "lost"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	result := readRevocationResult(t, raw)
	require.Equal(t, target.ID, result.UserID)
	require.True(t, result.Revoked)
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, result, readRevocationResult(t, raw))
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
}

func TestAgentUserRevocationLostPreparationCommitKeepsOriginalProposal(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	proxy := newCommitReplyProxy(t, h.store.Pool().Config().ConnString())
	store, err := db.Open(t.Context(), proxy.dsn)
	require.NoError(t, err)
	t.Cleanup(store.Close)
	h.store = store
	// Only this synchronous API may transact through the armed proxy. Queue
	// startup/shutdown commits do not belong to the preparation fault window.
	h.iam, err = identity.New(identity.Options{
		Issuer:                         h.peers.iam.URL,
		OrganizationID:                 "org",
		BotID:                          123,
		LoomAudience:                   "loom",
		APIAudience:                    "api",
		ActorID:                        "actor",
		ActorClientCredentials:         identity.ClientCredentials{ID: "actor-client", Secret: "secret"},
		ExchangeClientCredentials:      identity.ClientCredentials{ID: "exchange", Secret: "secret"},
		IntrospectionClientCredentials: identity.ClientCredentials{ID: "intro", Secret: "secret"},
	}, h.peers.iam.Client())
	require.NoError(t, err)
	h.endpoint, err = api.New(h.store, h.iam, bindingHTTPNoIntake{t}, api.Options{
		BotID:          123,
		WebhookSecret:  "test-secret",
		OrganizationID: "org",
		ActorID:        "actor",
		Issuer:         h.peers.iam.URL,
		LoomAudience:   "loom",
	})
	require.NoError(t, err)
	operator := settingsAdminToken(h, scope)
	proxy.armed.Store(true)
	code, raw := adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		userRevocationAPI+"/prepare",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "lost-preparation",
			"arguments":        revocationArguments(target),
		},
	)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	select {
	case <-proxy.dropped:
	case <-time.After(time.Second):
		t.Fatal("preparation COMMIT reply not dropped")
	}
	var original uuid.UUID
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT proposal_id FROM shuttle_user_revocation_proposals WHERE operation_id='lost-preparation'").
			Scan(&original),
	)
	body := prepareRevocationBody(t, h, operator, "lost-preparation", revocationArguments(target))
	preconditions := body["confirmation"].(map[string]any)["preconditions"].(map[string]any)
	require.Equal(t, original.String(), preconditions["proposal_id"])
	require.Zero(t, revocationAuditCount(t, h, target.ID))
	code, raw = adminRequest(t, h, operator, http.MethodPost, userRevocationAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.True(t, readRevocationResult(t, raw).Revoked)
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
}
