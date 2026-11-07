package integration_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
)

func revocationFixture(t *testing.T) (*harness, db.AdminScope, db.User) {
	t.Helper()
	h, scope, _ := settingsChangeFixture(t)
	target := scopedAccount(t, h, scope.BotID, 502, "revocation-target")
	return h, scope, target
}

func revocationAuditCount(t *testing.T, h *harness, id uuid.UUID) int64 {
	t.Helper()
	var count int64
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_identity_audit WHERE user_id=$1", id).
			Scan(&count),
	)
	return count
}

func TestOperatorRevocationKeepsRepeatedAuditsAndSelfRevocation(t *testing.T) {
	t.Parallel()
	h, scope, target := revocationFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	for range 2 {
		code, raw := adminRequest(
			t,
			h,
			operator,
			http.MethodPost,
			"/internal/admin/users/"+target.ID.String()+"/revoke",
			map[string]any{},
		)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Contains(t, string(raw), `"revoked":true`)
	}
	require.EqualValues(t, 2, revocationAuditCount(t, h, target.ID))
	code, raw := adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/admin/users/"+scope.UserID.String()+"/revoke",
		map[string]any{},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"revoked":true`)
	require.EqualValues(t, 1, revocationAuditCount(t, h, scope.UserID))
	code, raw = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/settings", nil)
	require.Equal(t, http.StatusForbidden, code, string(raw))
}

func TestOperatorRevocationLostCommitReplyInvalidatesBothIAMCaches(t *testing.T) {
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
	credential, err := h.iam.UserToken(t.Context(), target.Subject)
	require.NoError(t, err)
	_, err = h.iam.Verify(t.Context(), credential.Token)
	require.NoError(t, err)
	code, raw = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/settings", nil)
	require.Equal(t, http.StatusOK, code, string(raw))
	h.peers.mu.Lock()
	h.peers.iamDown = true
	h.peers.mu.Unlock()
	proxy.armed.Store(true)
	code, raw = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		"/internal/admin/users/"+target.ID.String()+"/revoke",
		map[string]any{},
	)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	select {
	case <-proxy.dropped:
	case <-time.After(time.Second):
		t.Fatal("actual revocation COMMIT reply was not dropped")
	}
	current, err := h.store.GetUser(t.Context(), target.ID)
	require.NoError(t, err)
	require.True(t, current.Revoked)
	require.EqualValues(t, 1, revocationAuditCount(t, h, target.ID))
	_, err = h.iam.Verify(t.Context(), credential.Token)
	require.ErrorIs(t, err, identity.ErrUnavailable, "positive authorization must have been invalidated")
	_, err = h.iam.UserToken(t.Context(), target.Subject)
	require.ErrorIs(t, err, identity.ErrUnavailable, "issued user-token cache must have been invalidated")
}
