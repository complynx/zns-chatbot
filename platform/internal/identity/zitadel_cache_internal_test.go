package identity

import (
	"context"
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
	rejectExchange  atomic.Bool
	rejectionCode   int
	rejectionBody   string
	verifyStarted   chan struct{}
	verifyRelease   chan struct{}
	exchangeStarted chan struct{}
	exchangeRelease chan struct{}
	verifyStatus    atomic.Int32
	failed          atomic.Bool
	inactive        atomic.Bool
	exchanges       atomic.Int32
	verifications   atomic.Int32
	exchangeToken   string
	lifetime        int64
	issuer          string
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
	if r.URL.Path == "/oauth/v2/token" {
		p.serveExchange(w, r)
	} else {
		p.serveVerification(w, r)
	}
}

func (p *cacheProvider) serveExchange(w http.ResponseWriter, r *http.Request) {
	if p.exchangeStarted != nil && r.Form.Get("subject_token") == "alice" {
		p.exchangeStarted <- struct{}{}
		select {
		case <-p.exchangeRelease:
		case <-r.Context().Done():
			return
		}
	}
	if p.rejectExchange.Load() {
		w.WriteHeader(p.rejectionCode)
		_, _ = w.Write([]byte(p.rejectionBody))
		return
	}
	token := p.exchangeToken
	if token == "" {
		token = "token-" + r.Form.Get("subject_token")
	}
	body := map[string]any{
		"access_token": token, "token_type": "Bearer",
		"issued_token_type": "urn:ietf:params:oauth:token-type:jwt", "expires_in": p.lifetime,
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (p *cacheProvider) serveVerification(w http.ResponseWriter, r *http.Request) {
	if p.verifyStarted != nil {
		p.verifyStarted <- struct{}{}
		select {
		case <-p.verifyRelease:
		case <-r.Context().Done():
			return
		}
	}
	if status := p.verifyStatus.Load(); status != 0 {
		w.WriteHeader(int(status))
		return
	}
	body := map[string]any{
		"active": !p.inactive.Load(), "sub": strings.TrimPrefix(r.Form.Get("token"), "token-"),
		"iss": p.issuer, "aud": []string{"project"}, "client_id": "bot-client",
		"exp": time.Now().Add(time.Duration(p.lifetime) * time.Second).Unix(),
		"act": map[string]string{"sub": "actor", "iss": p.issuer},
	}
	_ = json.NewEncoder(w).Encode(body)
}

func TestZitadelCancelledExchangeStillProcessesInactivity(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 3600)
	provider.rejectionCode = http.StatusBadRequest
	provider.rejectionBody = `{"error":"invalid_request","error_description":"Errors.User.NotActive"}`
	_, err := adapter.Verify(t.Context(), "token-alice")
	require.NoError(t, err)
	provider.exchangeStarted = make(chan struct{}, 1)
	provider.exchangeRelease = make(chan struct{})
	t.Cleanup(func() { close(provider.exchangeRelease) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, callErr := adapter.Exchange(ctx, "alice"); result <- callErr }()
	awaitCacheSignal(t, provider.exchangeStarted)
	adapter.exchanges.mu.Lock()
	flight := adapter.exchanges.flights["alice"]
	adapter.exchanges.mu.Unlock()
	require.NotNil(t, flight)
	cancel()
	require.ErrorIs(t, awaitCacheError(t, result), context.Canceled)
	provider.rejectExchange.Store(true)
	provider.exchangeRelease <- struct{}{}
	awaitCacheSignal(t, flight.done)
	provider.failed.Store(true)
	_, err = adapter.Verify(t.Context(), "token-alice")
	require.ErrorIs(t, err, ErrZitadelUnavailable, "detached provider denial must retire the cached verdict")
	require.Empty(t, adapter.verified.values)
}

func TestZitadelRetiredExchangePreservesProviderInactivity(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 3600)
	provider.rejectionCode = http.StatusBadRequest
	provider.rejectionBody = `{"error":"invalid_request","error_description":"Errors.User.NotActive"}`
	provider.exchangeStarted = make(chan struct{}, 1)
	provider.exchangeRelease = make(chan struct{})
	t.Cleanup(func() { close(provider.exchangeRelease) })
	provider.rejectExchange.Store(true)
	result := make(chan error, 1)
	go func() { _, err := adapter.Exchange(t.Context(), "alice"); result <- err }()
	awaitCacheSignal(t, provider.exchangeStarted)
	adapter.InvalidateSubject("alice")
	provider.exchangeRelease <- struct{}{}
	require.ErrorIs(t, awaitCacheError(t, result), ErrZitadelUserInactive)
	require.Empty(t, adapter.exchanges.values)
}

func TestZitadelRetiredVerificationPreservesProviderOutage(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 3600)
	provider.verifyStarted = make(chan struct{}, 1)
	provider.verifyRelease = make(chan struct{})
	t.Cleanup(func() { close(provider.verifyRelease) })
	provider.verifyStatus.Store(http.StatusServiceUnavailable)
	result := make(chan error, 1)
	go func() { _, err := adapter.Verify(t.Context(), "token-bob"); result <- err }()
	awaitCacheSignal(t, provider.verifyStarted)
	adapter.InvalidateSubject("alice")
	provider.verifyRelease <- struct{}{}
	require.ErrorIs(t, awaitCacheError(t, result), ErrZitadelUnavailable)
	require.Empty(t, adapter.verified.values)
}

func awaitCacheSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	const waitLimit = 2 * time.Second
	select {
	case <-signal:
	case <-time.After(waitLimit):
		t.Fatal("cache operation did not reach the expected boundary")
	}
}

func awaitCacheError(t *testing.T, result <-chan error) error {
	t.Helper()
	const waitLimit = 2 * time.Second
	select {
	case err := <-result:
		return err
	case <-time.After(waitLimit):
		t.Fatal("cache operation did not finish")
		return nil
	}
}

func TestZitadelInactiveExchangeEvictsVerifiedSubject(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 3600)
	provider.rejectionCode = http.StatusBadRequest
	provider.rejectionBody = `{"error":"invalid_request","error_description":"Errors.User.NotActive"}`
	for _, subject := range []string{"alice", "bob"} {
		token, err := adapter.Exchange(t.Context(), subject)
		require.NoError(t, err)
		_, err = adapter.Verify(t.Context(), token)
		require.NoError(t, err)
	}
	entry := adapter.exchanges.values["alice"]
	entry.until = time.Now().Add(-time.Second)
	adapter.exchanges.values["alice"] = entry
	provider.rejectExchange.Store(true)
	_, err := adapter.Exchange(t.Context(), "alice")
	require.ErrorIs(t, err, ErrZitadelUserInactive)
	provider.failed.Store(true)
	_, err = adapter.Verify(t.Context(), "token-alice")
	require.ErrorIs(t, err, ErrZitadelUnavailable, "observed inactivity must retire the still-fresh verdict")
	_, err = adapter.Exchange(t.Context(), "bob")
	require.NoError(t, err)
	_, err = adapter.Verify(t.Context(), "token-bob")
	require.NoError(t, err)
	require.Len(t, adapter.exchanges.values, 1)
	require.Len(t, adapter.verified.values, 1)
}

func TestZitadelExchangeFailurePreservesVerifiedSubject(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		code int
		body string
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error":"invalid_request","error_description":"Errors.User.NotActive"}`},
		{"outage", http.StatusServiceUnavailable, `{"error":"invalid_request","error_description":"Errors.User.NotActive"}`},
		{"actor", http.StatusBadRequest, `{"error":"invalid_client"}`},
		{"unknown", http.StatusBadRequest, `{"error":"invalid_request","error_description":"unknown"}`},
		{"malformed", http.StatusBadRequest, `{`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			adapter, provider := cacheAdapter(t, 3600)
			provider.rejectionCode, provider.rejectionBody = test.code, test.body
			token, err := adapter.Exchange(t.Context(), "alice")
			require.NoError(t, err)
			_, err = adapter.Verify(t.Context(), token)
			require.NoError(t, err)
			entry := adapter.exchanges.values["alice"]
			entry.until = time.Now().Add(-time.Second)
			adapter.exchanges.values["alice"] = entry
			provider.rejectExchange.Store(true)
			_, err = adapter.Exchange(t.Context(), "alice")
			require.ErrorIs(t, err, ErrZitadelUnavailable)
			provider.failed.Store(true)
			subject, err := adapter.Verify(t.Context(), token)
			require.NoError(t, err)
			require.Equal(t, "alice", subject)
			require.EqualValues(t, 1, provider.verifications.Load())
		})
	}
}

func TestZitadelInactiveExchangeRetiresPendingVerification(t *testing.T) {
	t.Parallel()
	for _, token := range []string{"token-alice", "token-unresolved"} {
		t.Run(token, func(t *testing.T) {
			t.Parallel()
			adapter, provider := cacheAdapter(t, 3600)
			provider.rejectionCode = http.StatusBadRequest
			provider.rejectionBody = `{"error":"invalid_request","error_description":"Errors.User.NotActive"}`
			_, err := adapter.Verify(t.Context(), "token-bob")
			require.NoError(t, err)
			_, err = adapter.Verify(t.Context(), "token-alice")
			require.NoError(t, err)
			for key, entry := range adapter.verified.values {
				if entry.subject == "alice" {
					entry.until = time.Now().Add(-time.Second)
					adapter.verified.values[key] = entry
				}
			}
			provider.verifyStarted = make(chan struct{}, 1)
			provider.verifyRelease = make(chan struct{})
			t.Cleanup(func() { close(provider.verifyRelease) })
			result := make(chan error, 1)
			go func() {
				_, verifyErr := adapter.Verify(t.Context(), token)
				result <- verifyErr
			}()
			const waitLimit = 2 * time.Second
			select {
			case <-provider.verifyStarted:
			case <-time.After(waitLimit):
				t.Fatal("introspection did not reach the provider")
			}
			provider.rejectExchange.Store(true)
			_, err = adapter.Exchange(t.Context(), "alice")
			require.ErrorIs(t, err, ErrZitadelUserInactive)
			provider.verifyRelease <- struct{}{}
			select {
			case err = <-result:
				require.ErrorIs(t, err, ErrZitadelIdentity)
			case <-time.After(waitLimit):
				t.Fatal("retired introspection did not finish")
			}
			require.Len(t, adapter.verified.values, 1, "late success must not restore a retired verdict")
			provider.failed.Store(true)
			_, err = adapter.Verify(t.Context(), "token-bob")
			require.NoError(t, err)
		})
	}
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

func TestZitadelFirstDenialUsesTrustedExchangeAssociation(t *testing.T) {
	t.Parallel()
	for _, token := range []string{"token-alice", "token-never-exchanged"} {
		t.Run(token, func(t *testing.T) {
			t.Parallel()
			adapter, provider := cacheAdapter(t, 3600)
			for _, subject := range []string{"alice", "bob"} {
				_, err := adapter.Exchange(t.Context(), subject)
				require.NoError(t, err)
			}
			provider.inactive.Store(true)
			_, err := adapter.Verify(t.Context(), token)
			require.ErrorIs(t, err, ErrZitadelIdentity)
			require.Empty(t, adapter.verified.values, "the first introspection never established a trusted subject")
			provider.failed.Store(true)
			aliceToken, err := adapter.Exchange(t.Context(), "alice")
			if token == "token-alice" {
				require.ErrorIs(t, err, ErrZitadelUnavailable, "a rejected exchanged token must not remain reusable")
				require.Empty(t, aliceToken)
				require.EqualValues(t, 3, provider.exchanges.Load(), "Alice must reach the provider after denial")
			} else {
				require.NoError(t, err, "an unassociated rejected token must not evict Alice")
				require.Equal(t, "token-alice", aliceToken)
				require.EqualValues(t, 2, provider.exchanges.Load())
			}
			bobToken, err := adapter.Exchange(t.Context(), "bob")
			require.NoError(t, err, "unrelated Bob retains his cached exchange during the outage")
			require.Equal(t, "token-bob", bobToken)
			require.EqualValues(t, 1, provider.verifications.Load())
		})
	}
}

func TestZitadelFirstDenialInvalidatesEveryMatchingExchange(t *testing.T) {
	t.Parallel()
	adapter, provider := cacheAdapter(t, 3600)
	provider.exchangeToken = "shared-exchanged-token"
	for _, subject := range []string{"alice", "bob"} {
		token, err := adapter.Exchange(t.Context(), subject)
		require.NoError(t, err)
		require.Equal(t, provider.exchangeToken, token)
	}
	provider.inactive.Store(true)
	_, err := adapter.Verify(t.Context(), provider.exchangeToken)
	require.ErrorIs(t, err, ErrZitadelIdentity)
	require.Empty(t, adapter.exchanges.values, "every trusted association of the rejected token is retired")
	provider.failed.Store(true)
	for _, subject := range []string{"alice", "bob"} {
		_, err = adapter.Exchange(t.Context(), subject)
		require.ErrorIs(t, err, ErrZitadelUnavailable)
	}
	require.EqualValues(t, 4, provider.exchanges.Load())
}
