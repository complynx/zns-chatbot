package identity

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ProfileName carries separate name components. A populated IAM profile does not
// establish whether a person confirmed the name or provisioning supplied it.
type ProfileName struct {
	FirstName string `json:"givenName"`
	LastName  string `json:"familyName"`
}

// ReadProfileName reads the exact active, verified synthetic identity. Callers
// supply a persisted binding and must enforce local ownership and revocation.
func (c *Client) ReadProfileName(ctx context.Context, account Account, telegramID int64) (ProfileName, error) {
	if !c.validProfileAccount(account, telegramID) {
		return ProfileName{}, ErrDenied
	}
	credential, err := c.ServiceToken(ctx)
	if err != nil {
		return ProfileName{}, err
	}
	return c.profileName(ctx, credential, account)
}

// SyncProfileName updates only given/family names and confirms the result by
// read-back. The caller owns durable retries, serial dispatch and local access
// checks. An unknown remote outcome must remain pending; it is not a rejection.
func (c *Client) SyncProfileName(
	ctx context.Context, account Account, telegramID int64, name ProfileName,
) error {
	if !c.validProfileAccount(account, telegramID) || !validProfileNamePart(name.FirstName) ||
		!validProfileNamePart(name.LastName) {
		return ErrDenied
	}
	credential, err := c.ServiceToken(ctx)
	if err != nil {
		return err
	}
	current, err := c.profileName(ctx, credential, account)
	if err != nil || current == name {
		return err
	}
	// PATCH preserves omitted profile fields, username, email and provider links.
	body := struct {
		Human struct {
			Profile ProfileName `json:"profile"`
		} `json:"human"`
	}{}
	body.Human.Profile = name
	var response struct{}
	if err = c.jsonRequest(ctx, http.MethodPatch, "/v2/users/"+url.PathEscape(account.Subject),
		body, credential, &response); err != nil {
		return err
	}
	current, err = c.profileName(ctx, credential, account)
	if err != nil {
		return err
	}
	if current != name {
		return ErrUnavailable
	}
	return nil
}

func (c *Client) validProfileAccount(account Account, telegramID int64) bool {
	return telegramID > 0 && telegramID <= maxTelegramID && validLinkValue(account.Subject) &&
		account.Subject != "." && account.Subject != ".." &&
		account.OrganizationID == c.options.OrganizationID &&
		account.SyntheticEmail == fmt.Sprintf("tg+%d+%d@telegram.invalid", c.options.BotID, telegramID)
}

func validProfileNamePart(value string) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 200 &&
		strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func (c *Client) profileName(ctx context.Context, credential Credential, account Account) (ProfileName, error) {
	var response struct {
		User iamUser `json:"user"`
	}
	if err := c.jsonRequest(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(account.Subject),
		nil, credential, &response); err != nil {
		return ProfileName{}, err
	}
	if response.User.ID != account.Subject {
		return ProfileName{}, ErrConflict
	}
	if _, err := c.account(response.User, account.SyntheticEmail); err != nil {
		return ProfileName{}, err
	}
	return response.User.Human.Profile, nil
}
