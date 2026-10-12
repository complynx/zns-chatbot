//nolint:testpackage // Private cache state and in-flight invalidation fences are part of these behavior tests.
package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/require"

	"github.com/complynx/shuttle/internal/telegram"
)

type iamFixture struct {
	mu                              sync.Mutex
	server                          *httptest.Server
	key                             *ecdsa.PrivateKey
	signer                          jose.Signer
	users                           []iamUser
	tokenStatus, introStatus        int
	active                          bool
	tokens, introspections, creates int
	tokenDelay                      time.Duration
	tokenStarted                    chan struct{}
	loseCreate                      bool
	forms                           []map[string][]string
	userTokenExpiry                 time.Time
	introRoleClaims                 map[string]any
}

func fixture(t *testing.T) (*iamFixture, *Client) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: key},
		(&jose.SignerOptions{}).WithHeader("kid", "fixture"),
	)
	require.NoError(t, err)
	f := &iamFixture{key: key, signer: signer, active: true}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	o := Options{
		Issuer:                         f.server.URL,
		OrganizationID:                 "org",
		BotID:                          123,
		LoomAudience:                   "loom",
		APIAudience:                    "api",
		ActorID:                        "actor",
		ActorClientCredentials:         ClientCredentials{"machine", "machine-secret"},
		ExchangeClientCredentials:      ClientCredentials{"exchange", "exchange-secret"},
		IntrospectionClientCredentials: ClientCredentials{"intro", "intro-secret"},
		PositiveCacheTTL:               time.Minute,
	}
	c, err := New(o, f.server.Client())
	require.NoError(t, err)
	return f, c
}
func (f *iamFixture) signed(subject, actor, org string, audience []string, expiry time.Time) string {
	s := claims{
		Issuer:   f.server.URL,
		Subject:  subject,
		Audience: audience,
		Expiry:   jwt.NewNumericDate(expiry),
		IssuedAt: jwt.NewNumericDate(time.Now()),
		Org:      org,
		Roles:    map[string]map[string]string{"account.me": {"org": "Org"}},
	}
	if actor != "" {
		s.Actor = &struct {
			Subject string `json:"sub"`
		}{actor}
	}
	result, err := jwt.Signed(f.signer).Claims(s).Serialize()
	if err != nil {
		panic(err)
	}
	return result
}

//nolint:gocognit,gocyclo,cyclop // The fixture models distinct IAM wire endpoints in one local handler.
func (f *iamFixture) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	respond := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		respond(
			discovery{
				Issuer:        f.server.URL,
				JWKS:          f.server.URL + "/keys",
				Token:         f.server.URL + "/token",
				Introspection: f.server.URL + "/intro",
			},
		)
	case "/keys":
		respond(
			jose.JSONWebKeySet{
				Keys: []jose.JSONWebKey{{Key: &f.key.PublicKey, KeyID: "fixture", Algorithm: "ES256", Use: "sig"}},
			},
		)
	case "/token":
		f.tokens++
		if f.tokenStarted != nil {
			select {
			case f.tokenStarted <- struct{}{}:
			default:
			}
		}
		_ = r.ParseForm()
		if !strings.Contains(r.Form.Get("scope"), "urn:zitadel:iam:org:projects:roles") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.forms = append(f.forms, r.Form)
		if f.tokenDelay > 0 {
			time.Sleep(f.tokenDelay)
		}
		if f.tokenStatus != 0 {
			w.WriteHeader(f.tokenStatus)
			_, _ = w.Write([]byte("secret upstream failure"))
			return
		}
		id, secret, ok := r.BasicAuth()
		subject, actor := "actor", ""
		if r.Form.Get("grant_type") == "client_credentials" {
			if !ok || id != "machine" || secret != "machine-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if !strings.Contains(r.Form.Get("scope"), "project:id:loom:aud") ||
				!strings.Contains(r.Form.Get("scope"), "project:id:api:aud") {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
		} else {
			if !ok || id != "exchange" || secret != "exchange-secret" ||
				r.Form.Get("subject_token_type") != "urn:zitadel:params:oauth:token-type:user_id" ||
				r.Form.Get("actor_token") == "" ||
				len(r.Form["audience"]) != 2 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			subject, actor = r.Form.Get("subject_token"), "actor"
		}
		expiry := time.Now().Add(time.Hour)
		if actor != "" && !f.userTokenExpiry.IsZero() {
			expiry = f.userTokenExpiry
		}
		respond(
			map[string]any{
				"access_token": f.signed(subject, actor, "org", []string{"loom", "api"}, expiry),
				"expires_in":   3600,
				"token_type":   "Bearer",
			},
		)
	case "/intro":
		f.introspections++
		if f.introStatus != 0 {
			w.WriteHeader(f.introStatus)
			return
		}
		id, secret, ok := r.BasicAuth()
		if !ok || id != "intro" || secret != "intro-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = r.ParseForm()
		parsed, err := jwt.ParseSigned(r.Form.Get("token"), []jose.SignatureAlgorithm{jose.ES256})
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var s claims
		if parsed.Claims(&f.key.PublicKey, &s) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b, _ := json.Marshal(s)
		var info map[string]any
		_ = json.Unmarshal(b, &info)
		info["active"] = f.active
		if f.introRoleClaims == nil {
			info["urn:zitadel:iam:org:project:api:roles"] = s.Roles
		} else {
			maps.Copy(info, f.introRoleClaims)
		}
		respond(info)
	case "/v2/users":
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.Header.Get("X-Zitadel-Orgid") != "org" ||
			!strings.Contains(string(body["queries"]), "organizationIdQuery") ||
			!strings.Contains(string(body["queries"]), "tg+123+456@telegram.invalid") {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		respond(map[string]any{"result": f.users, "details": map[string]any{"totalResult": "0"}})
	case "/v2/users/new":
		f.creates++
		var body struct {
			ID       string `json:"userId"`
			Org      string `json:"organizationId"`
			Username string `json:"username"`
			Human    struct {
				Email struct {
					Email    string `json:"email"`
					Verified bool   `json:"isVerified"`
				} `json:"email"`
			} `json:"human"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Org != "org" || body.Username != body.Human.Email.Email || !body.Human.Email.Verified {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.users = []iamUser{testUser(body.ID)}
		if f.loseCreate {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		respond(map[string]any{"id": body.ID})
	default:
		if strings.HasPrefix(r.URL.Path, "/v2/users/") {
			for _, u := range f.users {
				if r.URL.Path == "/v2/users/"+u.ID {
					respond(map[string]any{"user": u})
					return
				}
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}
}
func testUser(id string) iamUser {
	var u iamUser
	_ = json.Unmarshal(
		[]byte(
			`{"userId":"`+id+`","state":"USER_STATE_ACTIVE","details":{"resourceOwner":"org"},"human":{"email":{"email":"tg+123+456@telegram.invalid","isVerified":true}}}`,
		),
		&u,
	)
	return u
}
func TestEnsureUserCreatesAndReusesSyntheticIdentity(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	ctx := context.Background()
	a, err := c.EnsureUser(ctx, telegram.User{ID: 456, FirstName: "Daniel"}, "persisted-id")
	require.NoError(t, err)
	require.Equal(t, "persisted-id", a.Subject)
	require.Equal(t, "tg+123+456@telegram.invalid", a.SyntheticEmail)
	a, err = c.EnsureUser(ctx, telegram.User{ID: 456, FirstName: "Changed"}, "other-proposed-id")
	require.NoError(t, err)
	require.Equal(t, "persisted-id", a.Subject)
	require.Equal(t, 1, f.count("create"))
}
func TestEnsureUserReconcilesLostCreateResponse(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	f.loseCreate = true
	a, err := c.EnsureUser(context.Background(), telegram.User{ID: 456}, "persisted-id")
	require.NoError(t, err)
	require.Equal(t, "persisted-id", a.Subject)
	_, err = c.EnsureUser(context.Background(), telegram.User{ID: 456}, "persisted-id")
	require.NoError(t, err)
	require.Equal(t, 1, f.count("create"))
}
func TestEnsureUserRejectsAmbiguousOrWrongOrg(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"duplicates", "org", "unverified", "inactive"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, c := fixture(t)
			u := testUser("existing")
			switch kind {
			case "duplicates":
				f.users = []iamUser{u, u}
			case "org":
				u.Details.Org = "other"
				f.users = []iamUser{u}
			case "unverified":
				u.Human.Email.Verified = false
				f.users = []iamUser{u}
			case "inactive":
				u.State = "USER_STATE_LOCKED"
				f.users = []iamUser{u}
			}
			_, err := c.EnsureUser(context.Background(), telegram.User{ID: 456}, "id")
			require.Error(t, err)
			require.Equal(t, 0, f.count("create"))
		})
	}
}
func TestTokenExchangeSingleflightAndRenewal(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	ctx := context.Background()
	token, err := c.UserToken(ctx, "user")
	require.NoError(t, err)
	require.Equal(t, 2, f.count("token"))
	c.mu.Lock()
	cached := c.tokens["user"]
	cached.ExpiresAt = time.Now().Add(20 * time.Second)
	c.tokens["user"] = cached
	c.mu.Unlock()
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() { ; _, e := c.UserToken(ctx, "user"); errs <- e })
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	require.Equal(t, 3, f.count("token"))
	p, err := c.Verify(ctx, token.Token)
	require.NoError(t, err)
	require.Equal(t, "user", p.Subject)
	require.Equal(t, "actor", p.Actor)
}
func TestRenewalFallbackOnlyWhileValidAndTransient(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	ctx := context.Background()
	original, err := c.UserToken(ctx, "user")
	require.NoError(t, err)
	c.mu.Lock()
	cached := c.tokens["user"]
	cached.ExpiresAt = time.Now().Add(20 * time.Second)
	c.tokens["user"] = cached
	c.mu.Unlock()
	f.mu.Lock()
	f.tokenStatus = 503
	f.mu.Unlock()
	got, err := c.UserToken(ctx, "user")
	require.NoError(t, err)
	require.Equal(t, original.Token, got.Token)
	f.mu.Lock()
	f.tokenStatus = 400
	f.mu.Unlock()
	_, err = c.UserToken(ctx, "user")
	require.ErrorIs(t, err, ErrDenied)
	require.NotContains(t, err.Error(), "secret")
	f.mu.Lock()
	f.tokenStatus = 503
	f.mu.Unlock()
	_, err = c.UserToken(ctx, "user")
	require.ErrorIs(t, err, ErrUnavailable)
}
func TestRenewalTimeoutPreservesValidTokenButNotExpiredToken(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	_, err := c.UserToken(context.Background(), "user")
	require.NoError(t, err)
	c.mu.Lock()
	cached := c.tokens["user"]
	cached.ExpiresAt = time.Now().Add(20 * time.Second)
	c.tokens["user"] = cached
	c.mu.Unlock()
	f.mu.Lock()
	f.tokenDelay = 50 * time.Millisecond
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err = c.UserToken(ctx, "user")
	require.NoError(t, err)
	c.mu.Lock()
	cached.ExpiresAt = time.Now().Add(-time.Second)
	c.tokens["user"] = cached
	c.mu.Unlock()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	_, err = c.UserToken(ctx2, "user")
	require.ErrorIs(t, err, ErrUnavailable)
}
func TestVerifyCacheOutageAndKnownInactive(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	token, err := c.UserToken(context.Background(), "user")
	require.NoError(t, err)
	p, err := c.Verify(context.Background(), token.Token)
	require.NoError(t, err)
	p.Roles["account.me"]["org"] = "caller mutation"
	f.mu.Lock()
	f.introStatus = 503
	f.mu.Unlock()
	p, err = c.Verify(context.Background(), token.Token)
	require.NoError(t, err)
	require.Equal(t, "Org", p.Roles["account.me"]["org"])
	require.Equal(t, 1, f.count("intro"))
	require.NoError(t, c.SetPositiveCacheTTL(0))
	_, err = c.Verify(context.Background(), token.Token)
	require.ErrorIs(t, err, ErrUnavailable)
	f.mu.Lock()
	f.introStatus = 0
	f.mu.Unlock()
	f.mu.Lock()
	f.active = false
	f.mu.Unlock()
	_, err = c.Verify(context.Background(), token.Token)
	require.ErrorIs(t, err, ErrDenied)
	require.Empty(t, c.tokens["user"])
}
func TestVerifyRejectsJWTTrustMismatch(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	tests := []struct {
		name, actor, org string
		aud              []string
		expiry           time.Time
	}{{"org", "actor", "other", []string{"api"}, time.Now().Add(time.Hour)}, {"aud", "actor", "org", []string{"other"}, time.Now().Add(time.Hour)}, {"actor", "other", "org", []string{"api"}, time.Now().Add(time.Hour)}, {"expired", "actor", "org", []string{"api"}, time.Now().Add(-time.Second)}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			token := f.signed("user", test.actor, test.org, test.aud, test.expiry)
			_, err := c.Verify(context.Background(), token)
			require.ErrorIs(t, err, ErrDenied)
		})
	}
	require.Equal(t, 0, f.count("intro"))
}
func TestInvalidateDropsPositiveCache(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	token, err := c.UserToken(context.Background(), "user")
	require.NoError(t, err)
	_, err = c.Verify(context.Background(), token.Token)
	require.NoError(t, err)
	c.Invalidate("user")
	f.mu.Lock()
	f.introStatus = 503
	f.mu.Unlock()
	_, err = c.Verify(context.Background(), token.Token)
	require.ErrorIs(t, err, ErrUnavailable)
	_, ok := c.positive[sha256.Sum256([]byte(token.Token))]
	require.False(t, ok)
}

func TestInvalidationFencesInFlightExchange(t *testing.T) {
	t.Parallel()
	for _, subject := range []string{"user", "actor"} {
		t.Run(subject, func(t *testing.T) {
			t.Parallel()
			f, c := fixture(t)
			_, err := c.ServiceToken(context.Background())
			require.NoError(t, err)
			f.mu.Lock()
			f.tokenDelay = 50 * time.Millisecond
			f.tokenStarted = make(chan struct{}, 1)
			started := f.tokenStarted
			f.mu.Unlock()
			done := make(chan error, 1)
			go func() { _, exchangeErr := c.UserToken(context.Background(), "user"); done <- exchangeErr }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("exchange did not start")
			}
			c.Invalidate(subject)
			require.ErrorIs(t, <-done, ErrDenied)
			c.mu.Lock()
			_, ok := c.tokens["user"]
			c.mu.Unlock()
			require.False(t, ok)
		})
	}
}

func (f *iamFixture) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch kind {
	case "create":
		return f.creates
	case "token":
		return f.tokens
	default:
		return f.introspections
	}
}

func TestCredentialCacheEvictsEarliestUserAndPreservesService(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	ctx := context.Background()
	actor, err := c.ServiceToken(ctx)
	require.NoError(t, err)
	f.mu.Lock()
	f.userTokenExpiry = time.Now().Add(2 * time.Minute)
	f.mu.Unlock()
	_, err = c.UserToken(ctx, "earliest")
	require.NoError(t, err)
	f.mu.Lock()
	f.userTokenExpiry = time.Time{}
	f.mu.Unlock()
	for index := range maxIssuedCredentials - 1 {
		_, err = c.UserToken(ctx, "user-"+strconv.Itoa(index))
		require.NoError(t, err)
	}
	before := f.count("token")
	_, err = c.UserToken(ctx, "earliest")
	require.NoError(t, err)
	require.Equal(t, before+1, f.count("token"), "evicted user must obtain a new credential")
	c.mu.Lock()
	count := len(c.tokens)
	c.mu.Unlock()
	require.Equal(t, maxIssuedCredentials, count)
	f.mu.Lock()
	f.tokenStatus = http.StatusServiceUnavailable
	f.mu.Unlock()
	cachedActor, err := c.ServiceToken(ctx)
	require.NoError(t, err)
	require.Equal(t, actor.Token, cachedActor.Token)
	require.Equal(t, before+1, f.count("token"), "user cache pressure must preserve the service token")
}

func TestCredentialCachePrunesExpiredEntriesOnIssuance(t *testing.T) {
	t.Parallel()
	f, c := fixture(t)
	ctx := context.Background()
	f.mu.Lock()
	f.userTokenExpiry = time.Now().Add(2 * time.Second)
	f.mu.Unlock()
	expired, err := c.UserToken(ctx, "expired")
	require.NoError(t, err)
	time.Sleep(time.Until(expired.ExpiresAt) + 20*time.Millisecond)
	f.mu.Lock()
	f.userTokenExpiry = time.Time{}
	f.mu.Unlock()
	_, err = c.UserToken(ctx, "other")
	require.NoError(t, err)
	c.mu.Lock()
	_, retained := c.tokens["expired"]
	c.mu.Unlock()
	require.False(t, retained)
	before := f.count("token")
	_, err = c.UserToken(ctx, "expired")
	require.NoError(t, err)
	require.Equal(t, before+1, f.count("token"), "expired credential must be reissued")
}

func TestAuthorizationRolesBelongToConfiguredAPIProject(t *testing.T) {
	t.Parallel()
	admin := map[string]map[string]string{"shuttle.admin": {"org": "Org"}}
	cases := []struct {
		name              string
		roles             map[string]any
		permitted, denied bool
	}{
		{"API role", map[string]any{"urn:zitadel:iam:org:project:api:roles": admin}, true, false},
		{"flat role only", map[string]any{"urn:zitadel:iam:org:project:roles": admin}, false, false},
		{"Loom role with same name", map[string]any{
			"urn:zitadel:iam:org:project:roles":      admin,
			"urn:zitadel:iam:org:project:loom:roles": admin,
		}, false, false},
		{"API role removed", map[string]any{
			"urn:zitadel:iam:org:project:roles":     admin,
			"urn:zitadel:iam:org:project:api:roles": map[string]any{},
		}, false, false},
		{"malformed API role", map[string]any{"urn:zitadel:iam:org:project:api:roles": "admin"}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, client := fixture(t)
			f.mu.Lock()
			f.introRoleClaims = tc.roles
			f.mu.Unlock()
			token := f.signed("user", "actor", "org", []string{"loom", "api"}, time.Now().Add(time.Hour))
			principal, err := client.Verify(context.Background(), token)
			if tc.denied {
				require.ErrorIs(t, err, ErrDenied)
				return
			}
			require.NoError(t, err)
			_, granted := principal.Roles["shuttle.admin"]["org"]
			require.Equal(t, tc.permitted, granted)
		})
	}
}
