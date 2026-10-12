package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"golang.org/x/sync/singleflight"
)

var (
	ErrDenied      = errors.New("identity authorization denied")
	ErrUnavailable = errors.New("identity provider unavailable")
	ErrConflict    = errors.New("identity binding conflict")
	errNotFound    = errors.New("identity user not found")
)

const (
	requestTimeout             = 3 * time.Second
	maxResponseBytes           = 1 << 20
	maxCacheTTL                = 5 * time.Minute
	maxPositiveEntries         = 1024
	maxIssuedCredentials       = 4096
	maxCredentialSeconds int64 = 86400
	maxTelegramID        int64 = 4503599627370495
	maxUserIDLength            = 200
)

type ClientCredentials struct{ ID, Secret string }
type Options struct {
	Issuer, OrganizationID, LoomAudience, APIAudience, ActorID                        string
	BotID                                                                             int64
	ActorClientCredentials, ExchangeClientCredentials, IntrospectionClientCredentials ClientCredentials
	PositiveCacheTTL                                                                  time.Duration
}
type Account struct{ Subject, OrganizationID, SyntheticEmail string }
type Credential struct {
	Token     string
	ExpiresAt time.Time
}
type Principal struct {
	Subject, Actor, OrganizationID string
	ExpiresAt                      time.Time
	Roles                          map[string]map[string]string
}
type positive struct {
	principal Principal
	until     time.Time
}
type discovery struct {
	Issuer        string `json:"issuer"`
	JWKS          string `json:"jwks_uri"`
	Token         string `json:"token_endpoint"`
	Introspection string `json:"introspection_endpoint"`
}
type Client struct {
	cacheEpoch     uint64
	options        Options
	http           *http.Client
	mu             sync.Mutex
	discovery      discovery
	keys           jose.JSONWebKeySet
	keysUntil      time.Time
	nextKeyRefresh time.Time
	tokens         map[string]Credential
	positive       map[[32]byte]positive
	generation     map[string]uint64
	flight         singleflight.Group
}

func New(o Options, h *http.Client) (*Client, error) {
	u, err := url.Parse(o.Issuer)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && (u.Scheme != "http" || u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" && u.Hostname() != "::1")) {
		return nil, errors.New("valid identity issuer required")
	}
	if o.OrganizationID == "" || o.LoomAudience == "" || o.APIAudience == "" || o.ActorID == "" || o.BotID <= 0 ||
		o.BotID > maxTelegramID ||
		o.PositiveCacheTTL < 0 ||
		o.PositiveCacheTTL > maxCacheTTL {
		return nil, errors.New("identity org, audiences, actor, bot and bounded cache TTL required")
	}
	for _, v := range []ClientCredentials{o.ActorClientCredentials, o.ExchangeClientCredentials, o.IntrospectionClientCredentials} {
		if v.ID == "" || v.Secret == "" {
			return nil, errors.New("identity client credentials required")
		}
	}
	if h == nil {
		h = &http.Client{}
	}
	boundedClient := *h
	if boundedClient.Timeout == 0 || boundedClient.Timeout > requestTimeout {
		boundedClient.Timeout = requestTimeout
	}
	boundedClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		options:    o,
		http:       &boundedClient,
		tokens:     make(map[string]Credential),
		positive:   make(map[[32]byte]positive),
		generation: make(map[string]uint64),
	}, nil
}

// request never includes upstream response bodies or credentials in errors.
func (c *Client) request(
	ctx context.Context,
	method, endpoint string,
	body io.Reader,
	content string,
	creds ClientCredentials,
	bearer string,
	out any,
) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return ErrUnavailable
	}
	if content != "" {
		req.Header.Set("Content-Type", content)
	}
	if creds.ID != "" {
		req.SetBasicAuth(creds.ID, creds.Secret)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("X-Zitadel-Orgid", c.options.OrganizationID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		return ErrConflict
	}
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests {
		return ErrUnavailable
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrDenied
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return ErrUnavailable
	}
	if len(b) > maxResponseBytes || json.Unmarshal(b, out) != nil {
		return ErrDenied
	}
	return nil
}
func (c *Client) discover(ctx context.Context) (discovery, error) {
	c.mu.Lock()
	d := c.discovery
	c.mu.Unlock()
	if d.Issuer != "" {
		return d, nil
	}
	v, err, _ := c.flight.Do("discovery", func() (any, error) {
		c.mu.Lock()
		d = c.discovery
		c.mu.Unlock()
		if d.Issuer != "" {
			return d, nil
		}
		if err := c.request(
			ctx,
			http.MethodGet,
			strings.TrimSuffix(c.options.Issuer, "/")+"/.well-known/openid-configuration",
			nil,
			"",
			ClientCredentials{},
			"",
			&d,
		); err != nil {
			return d, err
		}
		root, _ := url.Parse(c.options.Issuer)
		if d.Issuer != c.options.Issuer {
			return d, ErrDenied
		}
		for _, s := range []string{d.JWKS, d.Token, d.Introspection} {
			u, e := url.Parse(s)
			if e != nil || u.Scheme != root.Scheme || u.Host != root.Host || u.User != nil || u.RawQuery != "" ||
				u.Fragment != "" {
				return d, ErrDenied
			}
		}
		c.mu.Lock()
		c.discovery = d
		c.mu.Unlock()
		return d, nil
	})
	if err != nil {
		return discovery{}, err
	}
	result, ok := v.(discovery)
	if !ok {
		return discovery{}, ErrUnavailable
	}
	return result, nil
}
func (c *Client) getKeys(ctx context.Context, endpoint string, force bool) (jose.JSONWebKeySet, error) {
	c.mu.Lock()
	keys, until := c.keys, c.keysUntil
	c.mu.Unlock()
	if !force && time.Now().Before(until) {
		return keys, nil
	}
	v, err, _ := c.flight.Do("jwks", func() (any, error) {
		c.mu.Lock()
		keys, until = c.keys, c.keysUntil
		c.mu.Unlock()
		if !force && time.Now().Before(until) {
			return keys, nil
		}
		if force {
			c.mu.Lock()
			if time.Now().Before(c.nextKeyRefresh) {
				c.mu.Unlock()
				return keys, nil
			}
			c.nextKeyRefresh = time.Now().Add(time.Second)
			c.mu.Unlock()
		}
		if e := c.request(ctx, http.MethodGet, endpoint, nil, "", ClientCredentials{}, "", &keys); e != nil {
			return keys, e
		}
		if len(keys.Keys) == 0 || len(keys.Keys) > 64 {
			return keys, ErrDenied
		}
		c.mu.Lock()
		c.keys = keys
		c.keysUntil = time.Now().Add(maxCacheTTL)
		c.mu.Unlock()
		return keys, nil
	})
	if err != nil {
		return jose.JSONWebKeySet{}, err
	}
	result, ok := v.(jose.JSONWebKeySet)
	if !ok {
		return jose.JSONWebKeySet{}, ErrUnavailable
	}
	return result, nil
}

type claims struct {
	jwt.Claims

	Org   string `json:"urn:zitadel:iam:user:resourceowner:id"`
	Actor *struct {
		Subject string `json:"sub"`
	} `json:"act"`
	Roles map[string]map[string]string `json:"urn:zitadel:iam:org:project:roles"`
}

func (c *Client) signed(ctx context.Context, token string) (claims, error) {
	var result claims
	if len(token) == 0 || len(token) > 16384 {
		return result, ErrDenied
	}
	parsed, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.RS256, jose.ES256})
	if err != nil || len(parsed.Headers) != 1 || parsed.Headers[0].KeyID == "" {
		return result, ErrDenied
	}
	d, err := c.discover(ctx)
	if err != nil {
		return result, err
	}
	keys, err := c.getKeys(ctx, d.JWKS, false)
	if err != nil {
		return result, err
	}
	kid := parsed.Headers[0].KeyID
	if !containsKey(keys, kid) {
		keys, err = c.getKeys(ctx, d.JWKS, true)
		if err != nil {
			return result, err
		}
	}
	if !verifyClaims(parsed, keys, kid, &result) || result.Expiry == nil || result.Subject == "" ||
		result.Org != c.options.OrganizationID ||
		result.ValidateWithLeeway(
			jwt.Expected{Issuer: c.options.Issuer, AnyAudience: jwt.Audience{c.options.APIAudience}, Time: time.Now()},
			0,
		) != nil {
		return claims{}, ErrDenied
	}
	if result.Actor != nil && result.Actor.Subject != c.options.ActorID {
		return claims{}, ErrDenied
	}
	if result.Subject == c.options.ActorID && result.Actor != nil {
		return claims{}, ErrDenied
	}
	return result, nil
}

// Verify checks JWT trust before using a bounded positive introspection result.
func (c *Client) Verify(ctx context.Context, token string) (Principal, error) {
	s, err := c.signed(ctx, token)
	if err != nil {
		return Principal{}, err
	}
	hash := sha256.Sum256([]byte(token))
	c.mu.Lock()
	cached, ok := c.positive[hash]
	generation := c.generation[s.Subject]
	epoch := c.cacheEpoch
	ttl := c.options.PositiveCacheTTL
	c.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return clonePrincipal(cached.principal), nil
	}
	d, err := c.discover(ctx)
	if err != nil {
		return Principal{}, err
	}
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	var payload json.RawMessage
	var info struct {
		claims

		Active bool `json:"active"`
	}
	err = c.request(
		ctx,
		http.MethodPost,
		d.Introspection,
		strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded",
		c.options.IntrospectionClientCredentials,
		"",
		&payload,
	)
	if err != nil {
		if errors.Is(err, ErrDenied) {
			c.Invalidate(s.Subject)
		}
		return Principal{}, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &info) != nil || json.Unmarshal(payload, &fields) != nil {
		c.Invalidate(s.Subject)
		return Principal{}, ErrDenied
	}
	// Flat roles belong to the introspection client's project, which can differ
	// from this API. Only the exact configured project claim may authorize it.
	var apiRoles map[string]map[string]string
	if raw, present := fields["urn:zitadel:iam:org:project:"+c.options.APIAudience+":roles"]; present {
		if json.Unmarshal(raw, &apiRoles) != nil {
			c.Invalidate(s.Subject)
			return Principal{}, ErrDenied
		}
	}
	if !info.Active || info.Subject != s.Subject || info.Issuer != s.Issuer || info.Org != s.Org ||
		info.Expiry == nil ||
		info.ValidateWithLeeway(
			jwt.Expected{Issuer: c.options.Issuer, AnyAudience: jwt.Audience{c.options.APIAudience}, Time: time.Now()},
			0,
		) != nil ||
		actorSubject(info.claims) != actorSubject(s) {
		c.Invalidate(s.Subject)
		return Principal{}, ErrDenied
	}
	expires := s.Expiry.Time()
	if info.Expiry.Time().Before(expires) {
		expires = info.Expiry.Time()
	}
	p := Principal{
		Subject:        s.Subject,
		Actor:          actorSubject(s),
		OrganizationID: s.Org,
		ExpiresAt:      expires,
		Roles:          apiRoles,
	}
	until := time.Now().Add(ttl)
	if expires.Before(until) {
		until = expires
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation[s.Subject] != generation || c.cacheEpoch != epoch {
		return Principal{}, ErrDenied
	}
	c.prunePositive()
	if ttl > 0 && len(c.positive) < maxPositiveEntries {
		c.positive[hash] = positive{clonePrincipal(p), until}
	}
	return p, nil
}
func actorSubject(s claims) string {
	if s.Actor != nil {
		return s.Actor.Subject
	}
	return s.Subject
}
func clonePrincipal(p Principal) Principal {
	roles := make(map[string]map[string]string, len(p.Roles))
	for role, orgs := range p.Roles {
		m := make(map[string]string, len(orgs))
		maps.Copy(m, orgs)
		roles[role] = m
	}
	p.Roles = roles
	return p
}
func (c *Client) Invalidate(subject string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation[subject]++
	delete(c.tokens, subject)
	for k, v := range c.positive {
		if v.principal.Subject == subject || v.principal.Actor == subject {
			delete(c.positive, k)
		}
	}
	if subject == c.options.ActorID {
		for s := range c.tokens {
			c.generation[s]++
			delete(c.tokens, s)
		}
		c.cacheEpoch++
	}
}
func (c *Client) ServiceToken(ctx context.Context) (Credential, error) {
	return c.token(ctx, c.options.ActorID, true)
}
func (c *Client) UserToken(ctx context.Context, subject string) (Credential, error) {
	if subject == "" || subject == c.options.ActorID {
		return Credential{}, ErrDenied
	}
	return c.token(ctx, subject, false)
}
func (c *Client) scopes(management bool) string {
	scopes := []string{
		"openid",
		"urn:zitadel:iam:user:resourceowner",
		"urn:zitadel:iam:org:projects:roles",
		"urn:zitadel:iam:org:project:id:" + c.options.LoomAudience + ":aud",
		"urn:zitadel:iam:org:project:id:" + c.options.APIAudience + ":aud",
	}
	if management {
		scopes = append(scopes, "urn:zitadel:iam:org:project:id:zitadel:aud")
	}
	return strings.Join(scopes, " ")
}
func (c *Client) token(ctx context.Context, subject string, service bool) (Credential, error) {
	c.mu.Lock()
	cached := c.tokens[subject]
	c.mu.Unlock()
	if time.Until(cached.ExpiresAt) > 30*time.Second {
		return cached, nil
	}
	value, err, _ := c.flight.Do("token:"+subject, func() (any, error) {
		c.mu.Lock()
		cached = c.tokens[subject]
		generation := c.generation[subject]
		actorGeneration := c.generation[c.options.ActorID]
		c.mu.Unlock()
		if time.Until(cached.ExpiresAt) > 30*time.Second {
			return cached, nil
		}
		result, e := c.issue(ctx, subject, service)
		if e != nil {
			return c.fallback(cached, subject, generation, actorGeneration, e)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.generation[subject] != generation || c.generation[c.options.ActorID] != actorGeneration {
			return Credential{}, ErrDenied
		}
		c.cacheCredential(subject, result)
		return result, nil
	})
	if err != nil {
		return Credential{}, err
	}
	result, ok := value.(Credential)
	if !ok {
		return Credential{}, ErrUnavailable
	}
	return result, nil
}
func (c *Client) issue(ctx context.Context, subject string, service bool) (Credential, error) {
	d, err := c.discover(ctx)
	if err != nil {
		return Credential{}, err
	}
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {c.scopes(true)}}
	creds := c.options.ActorClientCredentials
	if !service {
		actor, actorErr := c.ServiceToken(ctx)
		if actorErr != nil {
			return Credential{}, actorErr
		}
		creds = c.options.ExchangeClientCredentials
		form = url.Values{
			"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
			"subject_token":        {subject},
			"subject_token_type":   {"urn:zitadel:params:oauth:token-type:user_id"},
			"actor_token":          {actor.Token},
			"actor_token_type":     {"urn:ietf:params:oauth:token-type:access_token"},
			"requested_token_type": {"urn:ietf:params:oauth:token-type:jwt"},
			"audience":             {c.options.LoomAudience, c.options.APIAudience},
			"scope":                {c.scopes(false)},
		}
	}
	var response struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
		Type    string `json:"token_type"`
	}
	if err = c.request(
		ctx,
		http.MethodPost,
		d.Token,
		strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded",
		creds,
		"",
		&response,
	); err != nil {
		return Credential{}, err
	}
	if response.Token == "" || response.Expires <= 0 || !strings.EqualFold(response.Type, "Bearer") {
		return Credential{}, ErrDenied
	}
	s, err := c.signed(ctx, response.Token)
	if err != nil {
		return Credential{}, err
	}
	if s.Subject != subject || !s.Audience.Contains(c.options.LoomAudience) ||
		(!service && actorSubject(s) != c.options.ActorID) {
		return Credential{}, ErrDenied
	}
	expiry := s.Expiry.Time()
	durationExpiry := time.Now().Add(time.Duration(min(response.Expires, maxCredentialSeconds)) * time.Second)
	if durationExpiry.Before(expiry) {
		expiry = durationExpiry
	}
	return Credential{Token: response.Token, ExpiresAt: expiry}, nil
}

func (c *Client) jsonRequest(ctx context.Context, method, path string, body any, credential Credential, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return ErrDenied
		}
		reader = bytes.NewReader(b)
	}
	return c.request(
		ctx,
		method,
		strings.TrimSuffix(c.options.Issuer, "/")+path,
		reader,
		"application/json",
		ClientCredentials{},
		credential.Token,
		out,
	)
}

// SetPositiveCacheTTL applies the current DB setting without extending existing
// authorization checks. Token issuance caches retain their JWT expiry.
func (c *Client) SetPositiveCacheTTL(ttl time.Duration) error {
	if ttl < 0 || ttl > maxCacheTTL {
		return errors.New("identity cache TTL out of range")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.options.PositiveCacheTTL = ttl
	c.positive = make(map[[32]byte]positive)
	c.cacheEpoch++
	return nil
}

func containsKey(keys jose.JSONWebKeySet, kid string) bool {
	for _, key := range keys.Keys {
		if key.KeyID == kid {
			return true
		}
	}
	return false
}
func verifyClaims(parsed *jwt.JSONWebToken, keys jose.JSONWebKeySet, kid string, out *claims) bool {
	for _, key := range keys.Keys {
		if key.KeyID == kid && (key.Use == "" || key.Use == "sig") && parsed.Claims(key.Key, out) == nil {
			return true
		}
	}
	return false
}

// prunePositive runs under the client lock and bounds the authorization cache.
func (c *Client) prunePositive() {
	if len(c.positive) < maxPositiveEntries {
		return
	}
	for key, value := range c.positive {
		if !time.Now().Before(value.until) {
			delete(c.positive, key)
		}
	}
}

func (c *Client) fallback(
	cached Credential,
	subject string,
	generation, actorGeneration uint64,
	err error,
) (Credential, error) {
	if errors.Is(err, ErrDenied) {
		c.Invalidate(subject)
	}
	if errors.Is(err, ErrUnavailable) && cached.ExpiresAt.After(time.Now()) {
		c.mu.Lock()
		unchanged := c.generation[subject] == generation && c.generation[c.options.ActorID] == actorGeneration
		c.mu.Unlock()
		if unchanged {
			return cached, nil
		}
	}
	return Credential{}, err
}

// cacheCredential runs under the client lock. Preserve the service credential
// so user-cache pressure cannot disable every delegation during an IAM outage.
func (c *Client) cacheCredential(subject string, credential Credential) {
	now := time.Now()
	for cachedSubject, cached := range c.tokens {
		if !cached.ExpiresAt.After(now) {
			delete(c.tokens, cachedSubject)
		}
	}
	if _, exists := c.tokens[subject]; !exists && len(c.tokens) >= maxIssuedCredentials {
		var earliestSubject string
		var earliestExpiry time.Time
		for cachedSubject, cached := range c.tokens {
			if cachedSubject != c.options.ActorID &&
				(earliestSubject == "" || cached.ExpiresAt.Before(earliestExpiry)) {
				earliestSubject, earliestExpiry = cachedSubject, cached.ExpiresAt
			}
		}
		if earliestSubject != "" {
			delete(c.tokens, earliestSubject)
		}
	}
	c.tokens[subject] = credential
}
