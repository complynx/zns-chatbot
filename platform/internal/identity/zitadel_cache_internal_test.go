package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type cacheProvider struct {
	failed        atomic.Bool
	inactive      atomic.Bool
	exchanges     atomic.Int32
	verifications atomic.Int32
	lifetime      int64
	issuer        string
}

func (p *cacheProvider) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/oauth/v2/token" {
		p.exchanges.Add(1)
	} else {
		p.verifications.Add(1)
	}
	if p.failed.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if r.ParseForm() != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var body any
	if r.URL.Path == "/oauth/v2/token" {
		body = map[string]any{
			"access_token": "token-" + r.Form.Get("subject_token"), "token_type": "Bearer",
			"issued_token_type": "urn:ietf:params:oauth:token-type:jwt", "expires_in": p.lifetime,
		}
	} else {
		body = map[string]any{
			"active": !p.inactive.Load(), "sub": strings.TrimPrefix(r.Form.Get("token"), "token-"),
			"iss": p.issuer, "aud": []string{"project"}, "client_id": "bot-client",
			"exp": time.Now().Add(time.Duration(p.lifetime) * time.Second).Unix(),
			"act": map[string]string{"sub": "actor", "iss": p.issuer},
		}
	}
	_ = json.NewEncoder(w).Encode(body)
}

func cacheAdapter(t *testing.T, lifetime int64) (*Zitadel, *cacheProvider) {
	t.Helper()
	provider := &cacheProvider{lifetime: lifetime}
	server := httptest.NewTLSServer(http.HandlerFunc(provider.serve))
	t.Cleanup(server.Close)
	provider.issuer = server.URL
	adapter, err := NewZitadel(ZitadelConfig{
		Issuer: server.URL, Audience: "project", BotClientID: "bot-client", BotClientSecret: "bot-secret",
		APIClientID: "api-client", APIClientSecret: "api-secret", ActorID: "actor", ActorToken: "actor-token",
		HTTP: server.Client(),
	})
	require.NoError(t, err)
	return adapter, provider
}

func TestZitadelCachedAccessSurvivesOutageWithinFixedDeadline(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 3600)
	started := time.Now()
	token, err := adapter.Exchange(t.Context(), "alice")
	require.NoError(t, err)
	subject, err := adapter.Verify(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, "alice", subject)
	deadline := adapter.exchanges.values["alice"].until
	require.WithinDuration(t, started.Add(5*time.Minute), deadline, time.Second)
	for _, entry := range adapter.verified.values {
		require.WithinDuration(t, started.Add(5*time.Minute), entry.until, time.Second)
	}
	provider.failed.Store(true)
	_, err = adapter.Exchange(t.Context(), "alice")
	require.NoError(t, err)
	_, err = adapter.Verify(t.Context(), token)
	require.NoError(t, err)
	require.Equal(t, deadline, adapter.exchanges.values["alice"].until)
	require.EqualValues(t, 1, provider.exchanges.Load())
	require.EqualValues(t, 1, provider.verifications.Load())
	_, err = adapter.Exchange(t.Context(), "bob")
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	_, err = adapter.Verify(t.Context(), "token-bob")
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	adapter.InvalidateSubject("alice")
	_, err = adapter.Exchange(t.Context(), "alice")
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	_, err = adapter.Verify(t.Context(), token)
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	require.Empty(t, adapter.exchanges.values)
	require.Empty(t, adapter.verified.values)
}

func TestZitadelCacheCannotOutliveTokenOrAdapterScope(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 2)
	started := time.Now()
	token, err := adapter.Exchange(t.Context(), "alice")
	require.NoError(t, err)
	_, err = adapter.Verify(t.Context(), token)
	require.NoError(t, err)
	require.True(t, adapter.exchanges.values["alice"].until.Before(started.Add(2*time.Second)))
	for key, entry := range adapter.verified.values {
		require.Len(t, key, 64, "introspection keys contain only token digests")
		require.True(t, entry.until.Before(started.Add(2*time.Second)))
		entry.until = started.Add(-time.Second)
		adapter.verified.values[key] = entry
	}
	entry := adapter.exchanges.values["alice"]
	entry.until = started.Add(-time.Second)
	adapter.exchanges.values["alice"] = entry
	provider.failed.Store(true)
	_, err = adapter.Exchange(t.Context(), "alice")
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	_, err = adapter.Verify(t.Context(), token)
	require.ErrorIs(t, err, ErrZitadelUnavailable)
	other, otherProvider := cacheAdapter(t, 3600)
	otherProvider.failed.Store(true)
	_, err = other.Verify(t.Context(), token)
	require.ErrorIs(t, err, ErrZitadelUnavailable, "another adapter cannot inherit a positive verdict")
}

func TestZitadelObservedDenialEvictsTrustedSubject(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 3600)
	token, err := adapter.Exchange(t.Context(), "alice")
	require.NoError(t, err)
	_, err = adapter.Verify(t.Context(), token)
	require.NoError(t, err)
	for key, entry := range adapter.verified.values {
		entry.until = time.Now().Add(-time.Second)
		adapter.verified.values[key] = entry
	}
	provider.inactive.Store(true)
	_, err = adapter.Verify(t.Context(), token)
	require.ErrorIs(t, err, ErrZitadelIdentity)
	require.Empty(t, adapter.exchanges.values)
	require.Empty(t, adapter.verified.values)
	provider.failed.Store(true)
	_, err = adapter.Verify(t.Context(), token)
	require.ErrorIs(t, err, ErrZitadelUnavailable, "denial must not be cached as a positive verdict")
}
