package identity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/identity"
)

type prelinkProvider struct {
	server   *httptest.Server
	key      *rsa.PrivateKey
	mu       sync.Mutex
	requests int
	status   int
	body     string
	delay    time.Duration
	entered  chan struct{}
	release  chan struct{}
}

func newPrelinkProvider(t *testing.T) *prelinkProvider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	p := &prelinkProvider{key: key}
	keys, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		{Key: &key.PublicKey, KeyID: "provider-key", Algorithm: "RS256", Use: "sig"},
	}})
	require.NoError(t, err)
	p.body = string(keys)
	p.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.requests++
		status, body, delay := p.status, p.body, p.delay
		entered, release := p.entered, p.release
		p.mu.Unlock()
		assert.Equal(t, "/keys", r.URL.Path)
		assert.Empty(t, r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Cookie"))
		if entered != nil {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
		if release != nil {
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		if status != 0 {
			w.Header().Set("Location", p.server.URL+"/redirected")
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *prelinkProvider) verifier(t *testing.T) *identity.TelegramPrelinkVerifier {
	t.Helper()
	v, err := identity.NewTelegramPrelinkVerifier(identity.TelegramPrelinkOptions{
		Issuer: p.server.URL + "/tg", JWKSURL: p.server.URL + "/keys", Audience: "zinusya-prelink", BotID: 123,
	}, p.server.Client())
	require.NoError(t, err)
	return v
}

func (p *prelinkProvider) claims(now time.Time) map[string]any {
	return map[string]any{
		"iss":                            p.server.URL + "/tg",
		"aud":                            "zinusya-prelink",
		"sub":                            "telegram:123:opaque-not-456",
		"jti":                            "attempt-1",
		"iat":                            now.Unix(),
		"nbf":                            now.Add(-5 * time.Second).Unix(),
		"exp":                            now.Add(time.Minute).Unix(),
		"urn:zinusya:purpose":            "telegram-prelink",
		"urn:zitadeltg:telegram:bot_id":  "123",
		"urn:zitadeltg:telegram:user_id": "456",
		"urn:zitadeltg:telegram:subject": "opaque-not-456",
		"email":                          "tg+123+456@telegram.invalid",
		"email_verified":                 true,
		"given_name":                     "Alice",
		"family_name":                    "Fixture",
		"preferred_username":             "fixture",
	}
}

func (p *prelinkProvider) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return p.signRaw(t, string(payload), "provider-key", jose.RS256)
}

func (p *prelinkProvider) signRaw(t *testing.T, payload, kid string, algorithm jose.SignatureAlgorithm) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: algorithm, Key: p.key},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), kid))
	require.NoError(t, err)
	signed, err := signer.Sign([]byte(payload))
	require.NoError(t, err)
	compact, err := signed.CompactSerialize()
	require.NoError(t, err)
	return compact
}

func TestTelegramPrelinkProofPreservesIdentityAndCachedKeys(t *testing.T) {
	t.Parallel()
	provider := newPrelinkProvider(t)
	v := provider.verifier(t)
	now := time.Now().UTC().Truncate(time.Second)
	raw := provider.sign(t, provider.claims(now))
	for range 2 {
		proof, err := v.Verify(t.Context(), raw, now)
		require.NoError(t, err)
		require.Equal(t, int64(456), proof.User.ID)
		require.Equal(t, "Alice", proof.User.FirstName)
		require.Equal(t, "opaque-not-456", proof.ExternalSubject)
		require.Equal(t, "attempt-1", proof.JTI)
		require.Equal(t, sha256.Sum256([]byte(raw)), proof.Digest)
		require.Equal(t, now.Add(time.Minute), proof.ExpiresAt)
		provider.mu.Lock()
		provider.status = http.StatusServiceUnavailable
		provider.mu.Unlock()
	}
	provider.mu.Lock()
	require.Equal(t, 1, provider.requests)
	provider.mu.Unlock()
	// Cached keys never cache proof freshness or accept an expired assertion.
	_, err := v.Verify(t.Context(), raw, now.Add(time.Minute))
	require.ErrorIs(t, err, identity.ErrDenied)
}

func TestTelegramPrelinkProofRejectsWrongTrustOrScope(t *testing.T) {
	t.Parallel()
	p := newPrelinkProvider(t)
	v := p.verifier(t)
	now := time.Now().UTC().Truncate(time.Second)
	cases := []struct {
		name, field string
		value       any
	}{
		{"issuer", "iss", "https://other.invalid/tg"},
		{"relay audience", "aud", "zitadel"},
		{"multiple audiences", "aud", []string{"zinusya-prelink", "zitadel"}},
		{"missing audience", "aud", nil},
		{"purpose", "urn:zinusya:purpose", "login"},
		{"bot", "urn:zitadeltg:telegram:bot_id", "124"},
		{"noncanonical bot", "urn:zitadeltg:telegram:bot_id", "0123"},
		{"numeric bot type", "urn:zitadeltg:telegram:bot_id", 123},
		{"numeric user type", "urn:zitadeltg:telegram:user_id", 456},
		{"noncanonical user", "urn:zitadeltg:telegram:user_id", "0456"},
		{"negative user", "urn:zitadeltg:telegram:user_id", "-456"},
		{"unsafe user", "urn:zitadeltg:telegram:user_id", "4503599627370496"},
		{"invented subject", "sub", "telegram:123:456"},
		{"other opaque", "urn:zitadeltg:telegram:subject", "other"},
		{"blank opaque", "urn:zitadeltg:telegram:subject", ""},
		{"opaque whitespace", "urn:zitadeltg:telegram:subject", "opaque subject"},
		{"email", "email", "alice@example.org"},
		{"unverified", "email_verified", false},
		{"missing jti", "jti", nil},
		{"long jti", "jti", strings.Repeat("x", 201)},
		{"missing issued", "iat", nil},
		{"missing not before", "nbf", nil},
		{"missing expiry", "exp", nil},
		{"expired", "exp", now.Unix()},
		{"long lifetime", "exp", now.Add(61 * time.Second).Unix()},
		{"fractional issued", "iat", float64(now.Unix()) + 0.5},
		{"future not before", "nbf", now.Add(6 * time.Second).Unix()},
		{"old not before", "nbf", now.Add(-6 * time.Second).Unix()},
		{"profile control", "given_name", "Alice\nadmin"},
		{"profile object", "given_name", map[string]string{"name": "Alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			claims := p.claims(now)
			claims[tc.field] = tc.value
			_, err := v.Verify(t.Context(), p.sign(t, claims), now)
			require.ErrorIs(t, err, identity.ErrDenied)
		})
	}
	t.Run("future-issued", func(t *testing.T) {
		t.Parallel()
		claims := p.claims(now.Add(6 * time.Second))
		_, err := v.Verify(t.Context(), p.sign(t, claims), now)
		require.ErrorIs(t, err, identity.ErrDenied)
	})
	t.Run("singleton-array-and-skew", func(t *testing.T) {
		t.Parallel()
		claims := p.claims(now.Add(4 * time.Second))
		claims["aud"] = []string{"zinusya-prelink"}
		_, err := v.Verify(t.Context(), p.sign(t, claims), now)
		require.NoError(t, err)
	})
}

func TestTelegramPrelinkProofRejectsAmbiguousOrAlteredJWT(t *testing.T) {
	t.Parallel()
	p := newPrelinkProvider(t)
	v := p.verifier(t)
	now := time.Now().UTC().Truncate(time.Second)
	payload, err := json.Marshal(p.claims(now))
	require.NoError(t, err)
	duplicate := strings.TrimSuffix(string(payload), "}") + `,"\u006ati":"second"}`
	for _, raw := range []string{
		p.signRaw(t, duplicate, "provider-key", jose.RS256),
		p.signRaw(t, string(payload)+` {}`, "provider-key", jose.RS256),
		p.signRaw(t, string(payload), "provider-key", jose.RS512),
		strings.Repeat("x", (16<<10)+1),
		"a.b.c", "",
	} {
		_, err = v.Verify(t.Context(), raw, now)
		require.ErrorIs(t, err, identity.ErrDenied)
	}
	raw := p.sign(t, p.claims(now))
	parts := strings.Split(raw, ".")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(strings.ReplaceAll(string(payload), "456", "789")))
	_, err = v.Verify(t.Context(), strings.Join(parts, "."), now)
	require.ErrorIs(t, err, identity.ErrDenied)
	// A case alias cannot override or replace an exact protected claim.
	claims := p.claims(now)
	delete(claims, "iss")
	claims["ISS"] = p.server.URL + "/tg"
	_, err = v.Verify(t.Context(), p.sign(t, claims), now)
	require.ErrorIs(t, err, identity.ErrDenied)
}

//nolint:paralleltest,tparallel // Sequential failure schedule on one provider fixture.
func TestTelegramPrelinkKeysAreBoundedAndRedirectsAreDenied(t *testing.T) {
	t.Parallel()
	p := newPrelinkProvider(t)
	now := time.Now().UTC().Truncate(time.Second)
	raw := p.sign(t, p.claims(now))
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"outage", "", 503}, {"redirect", "", 302}, {"empty", `{"keys":[]}`, 200},
		{"malformed", `{`, 200}, {"oversized", strings.Repeat("x", (1<<20)+1), 200},
		{"duplicate root", `{"keys":[],"keys":[]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p.mu.Lock()
			p.status, p.body = tc.status, tc.body
			before := p.requests
			p.mu.Unlock()
			_, err := p.verifier(t).Verify(t.Context(), raw, now)
			require.ErrorIs(t, err, identity.ErrUnavailable)
			p.mu.Lock()
			require.Equal(t, before+1, p.requests)
			p.mu.Unlock()
		})
	}
}

func TestTelegramPrelinkUnknownKeyDoesNotFloodProvider(t *testing.T) {
	t.Parallel()
	p := newPrelinkProvider(t)
	v := p.verifier(t)
	now := time.Now().UTC().Truncate(time.Second)
	_, err := v.Verify(t.Context(), p.sign(t, p.claims(now)), now)
	require.NoError(t, err)
	payload, err := json.Marshal(p.claims(now))
	require.NoError(t, err)
	for n := range 12 {
		_, err = v.Verify(t.Context(), p.signRaw(t, string(payload), "unknown-"+strconv.Itoa(n), jose.RS256), now)
		require.ErrorIs(t, err, identity.ErrDenied)
	}
	p.mu.Lock()
	require.Equal(t, 1, p.requests)
	p.mu.Unlock()
}

func TestTelegramPrelinkRequiresHTTPSStartupTrust(t *testing.T) {
	t.Parallel()
	for _, o := range []identity.TelegramPrelinkOptions{
		{Issuer: "http://localhost", JWKSURL: "https://provider.invalid/keys", Audience: "prelink", BotID: 123},
		{Issuer: "https://provider.invalid", JWKSURL: "http://localhost/keys", Audience: "prelink", BotID: 123},
		{
			Issuer:   "https://user:password@provider.invalid",
			JWKSURL:  "https://provider.invalid/keys",
			Audience: "prelink",
			BotID:    123,
		},
		{
			Issuer:   "https://provider.invalid",
			JWKSURL:  "https://provider.invalid/keys?token=secret",
			Audience: "prelink",
			BotID:    123,
		},
		{Issuer: "https://provider.invalid", JWKSURL: "https://provider.invalid/keys", Audience: "", BotID: 123},
		{Issuer: "https://provider.invalid", JWKSURL: "https://provider.invalid/keys", Audience: "prelink", BotID: 0},
	} {
		_, err := identity.NewTelegramPrelinkVerifier(o, nil)
		require.Error(t, err)
	}
}

func TestTelegramPrelinkRejectsInvalidProviderKeys(t *testing.T) {
	t.Parallel()
	weak, keyErr := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, keyErr)
	for _, tc := range []struct {
		name string
		keys func(*rsa.PrivateKey) []jose.JSONWebKey
	}{
		{"weak", func(*rsa.PrivateKey) []jose.JSONWebKey {
			return []jose.JSONWebKey{{Key: &weak.PublicKey, KeyID: "provider-key"}}
		}},
		{"private", func(key *rsa.PrivateKey) []jose.JSONWebKey {
			return []jose.JSONWebKey{{Key: key, KeyID: "provider-key"}}
		}},
		{"encryption", func(key *rsa.PrivateKey) []jose.JSONWebKey {
			return []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "provider-key", Use: "enc"}}
		}},
		{"other algorithm", func(key *rsa.PrivateKey) []jose.JSONWebKey {
			return []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "provider-key", Algorithm: "RS512"}}
		}},
		{"duplicate kid", func(key *rsa.PrivateKey) []jose.JSONWebKey {
			item := jose.JSONWebKey{Key: &key.PublicKey, KeyID: "provider-key"}
			return []jose.JSONWebKey{item, item}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newPrelinkProvider(t)
			body, err := json.Marshal(jose.JSONWebKeySet{Keys: tc.keys(p.key)})
			require.NoError(t, err)
			p.mu.Lock()
			p.body = string(body)
			p.mu.Unlock()
			now := time.Now().UTC().Truncate(time.Second)
			_, err = p.verifier(t).Verify(t.Context(), p.sign(t, p.claims(now)), now)
			require.ErrorIs(t, err, identity.ErrUnavailable)
		})
	}
}

func TestTelegramPrelinkExpiresDuringKeyFetch(t *testing.T) {
	t.Parallel()
	p := newPrelinkProvider(t)
	p.mu.Lock()
	p.delay = 1100 * time.Millisecond
	p.mu.Unlock()
	now := time.Now().UTC().Truncate(time.Second)
	claims := p.claims(now)
	claims["exp"] = now.Add(time.Second).Unix()
	_, err := p.verifier(t).Verify(t.Context(), p.sign(t, claims), now)
	require.ErrorIs(t, err, identity.ErrDenied)
}

func TestTelegramPrelinkConcurrentProofsAndKeyRotation(t *testing.T) {
	t.Parallel()
	p := newPrelinkProvider(t)
	v := p.verifier(t)
	now := time.Now().UTC().Truncate(time.Second)
	raw := p.sign(t, p.claims(now))
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() {
			proof, err := v.Verify(t.Context(), raw, now)
			if err != nil {
				t.Errorf("verify concurrent proof: %v", err)
				return
			}
			assert.Equal(t, int64(456), proof.User.ID)
		})
	}
	group.Wait()
	p.mu.Lock()
	require.Equal(t, 1, p.requests)
	p.mu.Unlock()
	rotated, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	keys, err := json.Marshal(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
		{Key: &rotated.PublicKey, KeyID: "rotated", Use: "sig", Algorithm: "RS256"},
	}})
	require.NoError(t, err)
	p.mu.Lock()
	p.body = string(keys)
	p.mu.Unlock()
	// Unknown-key refresh is globally throttled, even for newly rotated keys.
	timer := time.NewTimer(5100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	case <-timer.C:
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: rotated},
		(&jose.SignerOptions{}).WithHeader(jose.HeaderKey("kid"), "rotated"))
	require.NoError(t, err)
	payload, err := json.Marshal(p.claims(time.Now().UTC().Truncate(time.Second)))
	require.NoError(t, err)
	signed, err := signer.Sign(payload)
	require.NoError(t, err)
	raw, err = signed.CompactSerialize()
	require.NoError(t, err)
	_, err = v.Verify(t.Context(), raw, time.Now())
	require.NoError(t, err)
	p.mu.Lock()
	require.Equal(t, 2, p.requests)
	p.mu.Unlock()
}

func TestTelegramPrelinkCanceledWaiterDoesNotAbortSharedRefresh(t *testing.T) {
	t.Parallel()
	p := newPrelinkProvider(t)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	p.mu.Lock()
	p.entered, p.release = entered, release
	p.mu.Unlock()
	v := p.verifier(t)
	now := time.Now().UTC().Truncate(time.Second)
	raw := p.sign(t, p.claims(now))
	firstCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := v.Verify(firstCtx, raw, now); first <- err }()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	cancel()
	require.ErrorIs(t, <-first, identity.ErrUnavailable)
	second := make(chan error, 1)
	go func() { _, err := v.Verify(t.Context(), raw, now); second <- err }()
	close(release)
	require.NoError(t, <-second)
	p.mu.Lock()
	require.Equal(t, 1, p.requests)
	p.mu.Unlock()
}
