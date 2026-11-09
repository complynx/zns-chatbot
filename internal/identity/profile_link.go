package identity

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const profileActiveState = "USER_STATE_ACTIVE"

func (c *Client) authorizeProfileAccount(
	ctx context.Context, credential Credential, user iamUser, account Account,
) (iamUser, error) {
	if user.Human == nil || user.Human.Email.Address == account.SyntheticEmail || account.TelegramLink == nil {
		_, err := c.account(user, account.SyntheticEmail)
		return user, err
	}
	// Reserved-domain identities must always satisfy the original exact,
	// verified synthetic-email contract, even when link evidence is supplied.
	if user.Human.Email.Address == "" ||
		strings.HasSuffix(strings.ToLower(user.Human.Email.Address), "@telegram.invalid") ||
		user.Details.Org != c.options.OrganizationID {
		return iamUser{}, ErrConflict
	}
	link := account.TelegramLink
	if !c.validProfileLink(account, link) {
		return iamUser{}, ErrDenied
	}
	expected := idpLink{Provider: link.ProviderID,
		Subject: "telegram:" + strconv.FormatInt(c.options.BotID, 10) + ":" + link.ExternalSubject}
	found, err := c.telegramLink(ctx, credential, account.Subject, expected)
	if err != nil {
		return iamUser{}, err
	}
	if !found {
		return iamUser{}, ErrConflict
	}
	// The link search is a separate request: refresh account state after it,
	// including when Sync will return early because the name is unchanged.
	// Return this same validated response: the earlier name may have changed
	// during link verification and must not acknowledge stale desired state.
	return c.linkedProfileUser(ctx, credential, account)
}

func (c *Client) linkedProfileUser(ctx context.Context, credential Credential, account Account) (iamUser, error) {
	var response struct {
		User iamUser `json:"user"`
	}
	if err := c.jsonRequest(ctx, http.MethodGet, "/v2/users/"+url.PathEscape(account.Subject),
		nil, credential, &response); err != nil {
		return iamUser{}, err
	}
	user := response.User
	if user.ID != account.Subject || user.Details.Org != c.options.OrganizationID || user.Human == nil {
		return iamUser{}, ErrConflict
	}
	if user.Human.Email.Address == account.SyntheticEmail {
		_, err := c.account(user, account.SyntheticEmail)
		return user, err
	}
	if user.Human.Email.Address == "" ||
		strings.HasSuffix(strings.ToLower(user.Human.Email.Address), "@telegram.invalid") {
		return iamUser{}, ErrConflict
	}
	if user.State != profileActiveState {
		return iamUser{}, ErrDenied
	}
	return user, nil
}

func (c *Client) validProfileLink(account Account, link *TelegramProfileLink) bool {
	issuer, providerID := c.TelegramProfileLinkPins()
	if link == nil || issuer == "" || providerID == "" {
		return false
	}
	prefix := "telegram:" + strconv.FormatInt(c.options.BotID, 10) + ":"
	return link.ProviderIssuer == issuer &&
		link.ProviderID == providerID && validLinkValue(link.ExternalSubject) &&
		len(prefix)+len(link.ExternalSubject) <= maxUserIDLength &&
		link.Subject == account.Subject && link.Organization == account.OrganizationID &&
		link.BotID == c.options.BotID && c.validProfileAccount(account, link.TelegramID) &&
		!link.CompletedAt.IsZero() && !link.ExpiresAt.IsZero() &&
		!link.CompletedAt.After(link.ExpiresAt) && !link.CompletedAt.After(time.Now())
}
