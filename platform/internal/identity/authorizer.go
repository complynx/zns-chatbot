package identity

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
)

const authorizerAudience = "zns-identity-provisioner"
const authorizerPurpose = "telegram_identity_link"
const MaxAuthorizerAssertionBytes = 16384
const authorizerJWKSTimeout = 3 * time.Second
const authorizerMaxJWKSBytes = 1 << 20
const httpsScheme = "https"

var ErrAuthorizer = errors.New("invalid authorizer assertion")

type AuthorizerConfig struct {
	Issuer, JWKSURL string
	BotID           int64
	AllowLocalHTTP  bool
}

type Authorizer struct {
	config AuthorizerConfig
	keys   keyfunc.Keyfunc
}

type AuthorizerIdentity struct {
	Telegram identityprovision.Telegram
	Subject  string
}

type authorizerClaims struct {
	jwt.RegisteredClaims

	Purpose         string `json:"purpose"`
	BotID           string `json:"urn:zitadeltg:telegram:bot_id"`
	TelegramSubject string `json:"urn:zitadeltg:telegram:subject"`
	TelegramID      string `json:"urn:zitadeltg:telegram:user_id"`
	FirstName       string `json:"given_name"`
	LastName        string `json:"family_name"`
	Language        string `json:"language_code"`
}

func NewAuthorizer(ctx context.Context, cfg AuthorizerConfig) (*Authorizer, error) {
	if !authorizerURL(cfg.Issuer, cfg.AllowLocalHTTP) || !authorizerURL(cfg.JWKSURL, cfg.AllowLocalHTTP) ||
		cfg.BotID <= 0 ||
		cfg.BotID >= 1<<52 {
		return nil, ErrAuthorizer
	}
	failInitially := false
	client := &http.Client{
		Timeout:       authorizerJWKSTimeout,
		Transport:     boundedJWKSTransport{},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	keys, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{cfg.JWKSURL}, keyfunc.Override{
		Client: client, HTTPTimeout: authorizerJWKSTimeout, RateLimitWaitMax: time.Second, RefreshInterval: time.Minute,
		NoErrorReturnFirstHTTPReq: &failInitially,
		RefreshErrorHandlerFunc:   func(string) func(context.Context, error) { return func(context.Context, error) {} },
	})
	if err != nil {
		return nil, ErrAuthorizer
	}
	return &Authorizer{config: cfg, keys: keys}, nil
}

func (a *Authorizer) Verify(ctx context.Context, assertion string) (AuthorizerIdentity, error) {
	if len(assertion) == 0 || len(assertion) > MaxAuthorizerAssertionBytes {
		return AuthorizerIdentity{}, ErrAuthorizer
	}
	claims := &authorizerClaims{}
	_, err := jwt.ParseWithClaims(
		assertion,
		claims,
		a.keys.KeyfuncCtx(ctx),
		jwt.WithValidMethods([]string{"RS256"}),
		jwt.WithIssuer(
			a.config.Issuer,
		),
		jwt.WithAudience(authorizerAudience),
		jwt.WithExpirationRequired(),
		jwt.WithNotBeforeRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil || !a.validClaims(claims) {
		return AuthorizerIdentity{}, ErrAuthorizer
	}
	id, err := strconv.ParseInt(claims.TelegramID, 10, 64)
	if err != nil || id <= 0 || id >= 1<<52 || strconv.FormatInt(id, 10) != claims.TelegramID {
		return AuthorizerIdentity{}, ErrAuthorizer
	}
	return AuthorizerIdentity{
		Telegram: identityprovision.Telegram{
			ID:        id,
			FirstName: claims.FirstName,
			LastName:  claims.LastName,
			Language:  claims.Language,
		},
		Subject: claims.Subject,
	}, nil
}

func (a *Authorizer) validClaims(c *authorizerClaims) bool {
	if c.IssuedAt == nil || c.ExpiresAt == nil || c.NotBefore == nil || c.Purpose != authorizerPurpose ||
		c.ID == "" || len(c.ID) > 128 || len(c.Audience) != 1 || c.BotID != strconv.FormatInt(a.config.BotID, 10) ||
		c.TelegramSubject == "" || len(c.TelegramSubject) > 512 || c.Subject != "telegram:"+c.BotID+":"+c.TelegramSubject {
		return false
	}
	lifetime := c.ExpiresAt.Sub(c.IssuedAt.Time)
	return lifetime > 0 && lifetime <= time.Minute && !c.NotBefore.Before(c.IssuedAt.Time) &&
		c.NotBefore.Before(c.ExpiresAt.Time)
}

func authorizerURL(raw string, allowLocalHTTP bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == httpsScheme {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return allowLocalHTTP && u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}

type boundedJWKSTransport struct{}
type boundedJWKSBody struct {
	io.Reader
	io.Closer
}

func (boundedJWKSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	response.Body = boundedJWKSBody{
		Reader: io.LimitReader(response.Body, authorizerMaxJWKSBytes),
		Closer: response.Body,
	}
	return response, nil
}
