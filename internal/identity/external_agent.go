package identity

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4/jwt"
)

// ExternalAgentSubject checks the original MCP token at the issuer using the
// MCP introspection client. The token is never used to call Zinusya tools.
func (c *Client) ExternalAgentSubject(ctx context.Context, token string) (Principal, error) {
	audience := c.options.ExternalAgentAudience
	if audience == "" || len(token) == 0 || len(token) > 16384 {
		return Principal{}, ErrDenied
	}
	d, err := c.discover(ctx)
	if err != nil {
		return Principal{}, err
	}
	form := url.Values{"token": {token}, "token_type_hint": {"access_token"}}
	var info struct {
		claims

		Active   bool   `json:"active"`
		ClientID string `json:"client_id"`
	}
	if err = c.request(ctx, http.MethodPost, d.Introspection, strings.NewReader(form.Encode()),
		"application/x-www-form-urlencoded", c.options.ExternalAgentIntrospectionCredentials, "", &info); err != nil {
		return Principal{}, err
	}
	if !info.Active || info.ClientID == "" || info.Subject == "" || info.Subject == c.options.ActorID ||
		info.Actor != nil || info.Org != c.options.OrganizationID || info.Expiry == nil ||
		info.Audience.Contains(c.options.APIAudience) || info.Audience.Contains(c.options.LoomAudience) ||
		info.ValidateWithLeeway(jwt.Expected{
			Issuer: c.options.Issuer, AnyAudience: jwt.Audience{audience}, Time: time.Now(),
		}, 0) != nil {
		return Principal{}, ErrDenied
	}
	return Principal{Subject: info.Subject, OrganizationID: info.Org, ExpiresAt: info.Expiry.Time()}, nil
}

// IssueExternalAgentToken issues a separate API credential for a verified MCP
// subject with a current local account. The issuer must confirm an active human.
func (c *Client) IssueExternalAgentToken(ctx context.Context, principal Principal) (Credential, error) {
	if c.options.ExternalAgentAudience == "" || principal.Subject == "" || principal.Subject == c.options.ActorID ||
		principal.OrganizationID != c.options.OrganizationID || !principal.ExpiresAt.After(time.Now()) {
		return Credential{}, ErrDenied
	}
	actor, err := c.ServiceToken(ctx)
	if err != nil {
		return Credential{}, err
	}
	var response struct {
		User iamUser `json:"user"`
	}
	if err = c.jsonRequest(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(principal.Subject),
		nil, actor, &response); err != nil {
		return Credential{}, err
	}
	user := response.User
	if user.ID != principal.Subject || user.Details.Org != principal.OrganizationID || user.Human == nil ||
		user.State != profileActiveState {
		return Credential{}, ErrDenied
	}
	credential, err := c.issue(ctx, principal.Subject, false, []string{c.options.APIAudience})
	// Bound Loom's cache to the source token. The issuer sets the JWT expiry.
	if principal.ExpiresAt.Before(credential.ExpiresAt) {
		credential.ExpiresAt = principal.ExpiresAt
	}
	return credential, err
}
