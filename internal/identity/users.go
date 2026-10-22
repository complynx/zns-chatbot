package identity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

const identitySearchLimit = 2

type iamUser struct {
	ID      string `json:"userId"`
	State   string `json:"state"`
	Details struct {
		Org string `json:"resourceOwner"`
	} `json:"details"`
	Human *struct {
		Profile ProfileName `json:"profile"`
		Email   struct {
			Address  string `json:"email"`
			Verified bool   `json:"isVerified"`
		} `json:"email"`
	} `json:"human"`
}

func (c *Client) account(u iamUser, email string) (Account, error) {
	if u.ID == "" || u.Details.Org != c.options.OrganizationID || u.Human == nil || u.Human.Email.Address != email ||
		!u.Human.Email.Verified {
		return Account{}, ErrConflict
	}
	if u.State != "USER_STATE_ACTIVE" {
		return Account{}, ErrDenied
	}
	return Account{Subject: u.ID, OrganizationID: u.Details.Org, SyntheticEmail: email}, nil
}
func (c *Client) find(ctx context.Context, email string, credential Credential) ([]iamUser, error) {
	body := map[string]any{
		"query": map[string]any{"limit": identitySearchLimit},
		"queries": []any{
			map[string]any{"emailQuery": map[string]any{"emailAddress": email, "method": "TEXT_QUERY_METHOD_EQUALS"}},
			map[string]any{"organizationIdQuery": map[string]any{"organizationId": c.options.OrganizationID}},
		},
	}
	var response struct {
		Result  []iamUser `json:"result"`
		Details struct {
			Total json.Number `json:"totalResult"`
		} `json:"details"`
	}
	if e := c.jsonRequest(ctx, http.MethodPost, "/v2/users", body, credential, &response); e != nil {
		return nil, e
	}
	if n, e := response.Details.Total.Int64(); e == nil && n > 1 {
		return nil, ErrConflict
	}
	if len(response.Result) > 1 {
		return nil, ErrConflict
	}
	return response.Result, nil
}

// EnsureUser requires a verified Telegram user and a caller-persisted proposed
// UUID under the caller's durable identity lock. Reuse that UUID after an
// unknown create result. This adapter never invents a Telegram OIDC subject.
func (c *Client) EnsureUser(ctx context.Context, u telegram.User, proposedUserID string) (Account, error) {
	if u.ID <= 0 || u.ID > maxTelegramID || u.IsBot || proposedUserID == "" || len(proposedUserID) > maxUserIDLength {
		return Account{}, ErrDenied
	}
	email := "tg+" + strconv.FormatInt(c.options.BotID, 10) + "+" + strconv.FormatInt(u.ID, 10) + "@telegram.invalid"
	credential, err := c.ServiceToken(ctx)
	if err != nil {
		return Account{}, err
	}
	users, err := c.find(ctx, email, credential)
	if err != nil {
		return Account{}, err
	}
	if len(users) == 1 {
		return c.account(users[0], email)
	}
	// Check the caller's immutable ID before creation: the email projection can
	// lag after a previous create request whose response was lost.
	existing, readErr := c.getAccount(ctx, credential, proposedUserID, email)
	if readErr == nil {
		return existing, nil
	}
	if !errors.Is(readErr, errNotFound) {
		return Account{}, readErr
	}
	given, family := strings.TrimSpace(u.FirstName), strings.TrimSpace(u.LastName)
	if given == "" {
		given = "Telegram user"
	}
	if family == "" {
		family = given
	}
	body := map[string]any{
		"organizationId": c.options.OrganizationID,
		"userId":         proposedUserID,
		"username":       email,
		"human": map[string]any{
			"profile": map[string]any{"givenName": given, "familyName": family},
			"email":   map[string]any{"email": email, "isVerified": true},
		},
	}
	var created struct {
		ID string `json:"id"`
	}
	createErr := c.jsonRequest(ctx, http.MethodPost, "/v2/users/new", body, credential, &created)
	if createErr != nil && !errors.Is(createErr, ErrUnavailable) && !errors.Is(createErr, ErrConflict) {
		return Account{}, createErr
	}
	if createErr == nil && created.ID != proposedUserID {
		return Account{}, ErrConflict
	}
	if reconciled, e := c.getAccount(ctx, credential, proposedUserID, email); e == nil {
		return reconciled, nil
	}
	users, err = c.find(ctx, email, credential)
	if err == nil && len(users) == 1 {
		return c.account(users[0], email)
	}
	if err != nil {
		return Account{}, err
	}
	if createErr != nil {
		return Account{}, createErr
	}
	return Account{}, ErrUnavailable
}

func (c *Client) getAccount(ctx context.Context, credential Credential, userID, email string) (Account, error) {
	var response struct {
		User iamUser `json:"user"`
	}
	if err := c.jsonRequest(
		ctx,
		http.MethodGet,
		"/v2/users/"+url.PathEscape(userID),
		nil,
		credential,
		&response,
	); err != nil {
		return Account{}, err
	}
	if response.User.ID != userID {
		return Account{}, ErrConflict
	}
	return c.account(response.User, email)
}
