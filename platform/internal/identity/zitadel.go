package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// ZitadelConfig separates the confidential bot app from the API introspection
// app. The actor is a dedicated service account with scoped impersonation rights.
type ZitadelConfig struct {
	Issuer            string
	Audience          string
	BotClientID       string
	BotClientSecret   string
	APIClientID       string
	APIClientSecret   string
	ActorID           string
	ActorToken        string
	ActorClientID     string
	ActorClientSecret string
	HTTP              *http.Client
	Sandbox           bool
}

type Zitadel struct {
	config       ZitadelConfig
	http         http.Client
	actorMu      sync.Mutex
	actorToken   string
	actorRefresh time.Time
}

var ErrZitadelIdentity = errors.New("invalid Zitadel identity")

// ErrZitadelUserInactive is an explicit rejection of the exchanged user, not an
// actor credential, provider configuration or transport failure.
var ErrZitadelUserInactive = errors.New("Zitadel user inactive")

func NewZitadel(config ZitadelConfig) (*Zitadel, error) {
	u, err := url.Parse(config.Issuer)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" ||
		(u.Scheme != httpsScheme && (!config.Sandbox || u.Scheme != "http")) {
		return nil, errors.New("invalid Zitadel issuer")
	}
	for _, value := range []string{config.Audience, config.BotClientID, config.BotClientSecret,
		config.APIClientID, config.APIClientSecret, config.ActorID} {
		if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("incomplete Zitadel configuration")
		}
	}
	if config.ActorToken != "" {
		if config.ActorClientID != "" || config.ActorClientSecret != "" ||
			strings.ContainsAny(config.ActorToken, "\r\n") ||
			strings.TrimSpace(config.ActorToken) == "" {
			return nil, errors.New("invalid Zitadel actor configuration")
		}
	} else {
		for _, value := range []string{config.ActorClientID, config.ActorClientSecret} {
			if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
				return nil, errors.New("incomplete Zitadel actor configuration")
			}
		}
	}
	const timeout = 10 * time.Second
	client := http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	if config.HTTP != nil {
		client.Transport = config.HTTP.Transport
	}
	return &Zitadel{config: config, http: client}, nil
}

// Exchange is called only after resolving a trusted Telegram sender to a stored
// Zitadel subject. Neither model output nor request JSON may choose that subject.
func (z *Zitadel) Exchange(ctx context.Context, subject string) (string, error) {
	if subject == "" || len(subject) > 256 || strings.ContainsAny(subject, "\r\n") {
		return "", ErrZitadelIdentity
	}
	actorToken, err := z.getActorToken(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":           {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"subject_token":        {subject},
		"subject_token_type":   {"urn:zitadel:params:oauth:token-type:user_id"},
		"actor_token":          {actorToken},
		"actor_token_type":     {"urn:ietf:params:oauth:token-type:access_token"},
		"requested_token_type": {"urn:ietf:params:oauth:token-type:jwt"},
		"scope":                {"openid urn:zitadel:iam:org:project:id:" + z.config.Audience + ":aud"},
		"audience":             {z.config.Audience},
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		IssuedType  string `json:"issued_token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err = z.post(ctx, "/oauth/v2/token", z.config.BotClientID, z.config.BotClientSecret, form, &token); err != nil {
		return "", err
	}
	if token.AccessToken == "" || len(token.AccessToken) > 16<<10 || token.TokenType != "Bearer" ||
		token.ExpiresIn <= 0 ||
		token.IssuedType != "urn:ietf:params:oauth:token-type:jwt" {
		return "", ErrZitadelIdentity
	}
	return token.AccessToken, nil
}

// getActorToken shares one audience-scoped token across exchanges, renewing at
// 90% of its lifetime. Failed renewal never falls back to the previous token.
func (z *Zitadel) getActorToken(ctx context.Context) (string, error) {
	if z.config.ActorToken != "" {
		return z.config.ActorToken, nil
	}
	z.actorMu.Lock()
	defer z.actorMu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if z.actorToken != "" && time.Now().Before(z.actorRefresh) {
		return z.actorToken, nil
	}
	started := time.Now()
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	form := url.Values{
		"grant_type": {"client_credentials"},
		"scope":      {"openid urn:zitadel:iam:org:project:id:" + z.config.Audience + ":aud"},
	}
	if err := z.post(
		ctx,
		"/oauth/v2/token",
		z.config.ActorClientID,
		z.config.ActorClientSecret,
		form,
		&token,
	); err != nil {
		return "", err
	}
	// Bound lifetime before converting seconds to a time.Duration.
	const maxLifetime = int64(365 * 24 * 60 * 60)
	if token.AccessToken == "" || len(token.AccessToken) > 16<<10 || strings.ContainsAny(token.AccessToken, "\r\n") ||
		token.TokenType != "Bearer" || token.ExpiresIn <= 0 || token.ExpiresIn > maxLifetime {
		return "", ErrZitadelIdentity
	}
	const renewalNumerator, renewalDenominator = 9, 10
	refresh := started.Add(time.Duration(token.ExpiresIn) * time.Second * renewalNumerator / renewalDenominator)
	if !time.Now().Before(refresh) {
		return "", ErrZitadelIdentity
	}
	z.actorToken, z.actorRefresh = token.AccessToken, refresh
	return token.AccessToken, nil
}

// Verify performs authenticated introspection for each API request. Audience is
// necessary but does not grant business permissions; domain services check those.
func (z *Zitadel) Verify(ctx context.Context, token string) (string, error) {
	if token == "" || len(token) > 16<<10 || strings.ContainsAny(token, "\r\n") {
		return "", ErrZitadelIdentity
	}
	var claims struct {
		Active    bool     `json:"active"`
		Subject   string   `json:"sub"`
		Issuer    string   `json:"iss"`
		Audience  []string `json:"aud"`
		ClientID  string   `json:"client_id"`
		Expires   int64    `json:"exp"`
		NotBefore int64    `json:"nbf"`
		Actor     struct {
			Subject string `json:"sub"`
			Issuer  string `json:"iss"`
		} `json:"act"`
	}
	if err := z.post(
		ctx,
		"/oauth/v2/introspect",
		z.config.APIClientID,
		z.config.APIClientSecret,
		url.Values{"token": {token}},
		&claims,
	); err != nil {
		return "", err
	}
	now := time.Now().Unix()
	if !claims.Active || claims.Subject == "" || claims.Issuer != z.config.Issuer ||
		!slices.Contains(claims.Audience, z.config.Audience) ||
		claims.ClientID != z.config.BotClientID ||
		claims.Expires <= now ||
		claims.NotBefore > now ||
		claims.Actor.Subject != z.config.ActorID ||
		(claims.Actor.Issuer != "" && claims.Actor.Issuer != z.config.Issuer) {
		return "", ErrZitadelIdentity
	}
	return claims.Subject, nil
}

func (z *Zitadel) post(ctx context.Context, path, clientID, secret string, form url.Values, out any) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		z.config.Issuer+path,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return ErrZitadelIdentity
	}
	request.SetBasicAuth(url.QueryEscape(clientID), url.QueryEscape(secret))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := z.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("Zitadel unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		if inactiveExchange(response, form) {
			return ErrZitadelUserInactive
		}
		return ErrZitadelIdentity
	}
	const maxResponse = 64 << 10
	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return ErrZitadelIdentity
	}
	if json.Unmarshal(data, out) != nil {
		return ErrZitadelIdentity
	}
	return nil
}

// Match the pinned provider's inactive-user response only for user exchange.
// Unknown OAuth errors must remain retryable rather than discard an update.
func inactiveExchange(response *http.Response, form url.Values) bool {
	if response.StatusCode != http.StatusBadRequest ||
		form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:token-exchange" {
		return false
	}
	const maxErrorResponse = 4096
	data, err := io.ReadAll(io.LimitReader(response.Body, maxErrorResponse+1))
	if err != nil || len(data) > maxErrorResponse {
		return false
	}
	var rejection struct {
		Code        string `json:"error"`
		Description string `json:"error_description"`
	}
	return json.Unmarshal(data, &rejection) == nil && rejection.Code == "invalid_request" &&
		rejection.Description == "Errors.User.NotActive"
}
