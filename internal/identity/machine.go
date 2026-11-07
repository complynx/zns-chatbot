package identity

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const grantTypeKey = "grant_type"
const scopeKey = "scope"

func validIdentityIssuer(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && u.User == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		(u.Scheme == "https" || u.Scheme == "http" &&
			(u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))
}

// MachineOptions selects one own-subject machine, without human delegation.
type MachineOptions struct {
	Issuer, OrganizationID, Audience, Subject string
	Credentials                               ClientCredentials
}

// MachineTokenSource keeps short-lived credentials in memory, shared by worker slots.
type MachineTokenSource struct {
	mu         sync.Mutex
	client     *Client
	options    MachineOptions
	credential Credential
}

// NewMachineTokenSource pins JWT identity and the sole requested service audience.
func NewMachineTokenSource(o MachineOptions, h *http.Client) (*MachineTokenSource, error) {
	if !validIdentityIssuer(o.Issuer) {
		return nil, errors.New("valid machine identity issuer required")
	}
	for _, value := range []string{o.OrganizationID, o.Audience, o.Subject, o.Credentials.ID, o.Credentials.Secret} {
		if strings.TrimSpace(value) == "" || len(value) > 4096 || strings.ContainsAny(value, "\r\n") {
			return nil, errors.New("machine identity configuration incomplete")
		}
	}
	if h == nil {
		h = &http.Client{}
	}
	bounded := *h
	if bounded.Timeout == 0 || bounded.Timeout > requestTimeout {
		bounded.Timeout = requestTimeout
	}
	bounded.Jar = nil
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &MachineTokenSource{options: o, client: &Client{
		options: Options{Issuer: o.Issuer, OrganizationID: o.OrganizationID, APIAudience: o.Audience, ActorID: o.Subject},
		http:    &bounded,
	}}, nil
}

// Token refreshes before a bounded procedure can outlive the cached JWT.
// Rejected or uncertain token replies never replace the current credential.
func (s *MachineTokenSource) Token(ctx context.Context, minimumLifetime time.Duration) (Credential, error) {
	if minimumLifetime < time.Minute || minimumLifetime >= time.Duration(maxCredentialSeconds)*time.Second {
		return Credential{}, errors.New("invalid machine token lifetime requirement")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Credential{}, err
	}
	if s.credential.ExpiresAt.After(time.Now().Add(minimumLifetime)) {
		return s.credential, nil
	}
	d, err := s.client.discover(ctx)
	if err != nil {
		return Credential{}, err
	}
	form := url.Values{grantTypeKey: {"client_credentials"}, scopeKey: {strings.Join([]string{
		"openid", "urn:zitadel:iam:user:resourceowner", "urn:zitadel:iam:org:projects:roles",
		"urn:zitadel:iam:org:project:id:" + s.options.Audience + ":aud",
	}, " ")}}
	var reply struct {
		Token   string `json:"access_token"`
		Expires int64  `json:"expires_in"`
		Type    string `json:"token_type"`
	}
	started := time.Now()
	err = s.client.request(ctx, http.MethodPost, d.Token, strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded", s.options.Credentials, "", &reply)
	if err != nil {
		return Credential{}, err
	}
	if !strings.EqualFold(reply.Type, "bearer") || reply.Expires <= 0 || reply.Expires > maxCredentialSeconds {
		return Credential{}, ErrDenied
	}
	verified, err := s.client.signed(ctx, reply.Token)
	if err != nil {
		return Credential{}, err
	}
	if verified.Subject != s.options.Subject || verified.Actor != nil {
		return Credential{}, ErrDenied
	}
	expires := started.Add(time.Duration(reply.Expires) * time.Second)
	if verified.Expiry.Time().Before(expires) {
		expires = verified.Expiry.Time()
	}
	if !expires.After(time.Now().Add(minimumLifetime)) {
		return Credential{}, ErrDenied
	}
	s.credential = Credential{Token: reply.Token, ExpiresAt: expires}
	return s.credential, nil
}
