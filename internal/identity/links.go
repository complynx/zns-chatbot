package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const maxIdentityLinks = 100

type idpLink struct {
	Provider string `json:"idpId"`
	Subject  string `json:"userId"`
	Name     string `json:"userName"`
}

// EnsureTelegramIDPLink reconciles one verified provider identity onto the exact
// active IAM account. The caller must verify Telegram proof, pin providerID and
// persist the intended tuple before calling; this is not an agent/browser API.
// A successful write or conflict is insufficient without exact link read-back.
// The caller must still recheck local revocation before completing its receipt.
func (c *Client) EnsureTelegramIDPLink(
	ctx context.Context, account Account, telegramID int64, providerID, opaqueSubject string,
) error {
	prefix := "telegram:" + strconv.FormatInt(c.options.BotID, 10) + ":"
	if telegramID <= 0 || telegramID > maxTelegramID || !validLinkValue(providerID) ||
		!validLinkValue(opaqueSubject) || len(prefix)+len(opaqueSubject) > maxUserIDLength ||
		!validLinkValue(account.Subject) || account.OrganizationID != c.options.OrganizationID {
		return ErrDenied
	}
	email := fmt.Sprintf("tg+%d+%d@telegram.invalid", c.options.BotID, telegramID)
	if account.SyntheticEmail != email {
		return ErrDenied
	}
	credential, err := c.ServiceToken(ctx)
	if err != nil {
		return err
	}
	if _, err = c.getAccount(ctx, credential, account.Subject, email); err != nil {
		return err
	}
	expected := idpLink{Provider: providerID, Subject: prefix + opaqueSubject, Name: email}
	found, err := c.telegramLink(ctx, credential, account.Subject, expected)
	if err != nil {
		return err
	}
	if !found {
		if err = c.addTelegramLink(ctx, credential, account.Subject, expected); err != nil {
			return err
		}
	}

	// Link projection may lag an acknowledged write; account status must also be
	// fresh. Neither this read nor the remote write is atomic with local revocation.
	_, err = c.getAccount(ctx, credential, account.Subject, email)
	return err
}

func validLinkValue(value string) bool {
	return value != "" && len(value) <= maxUserIDLength && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.IsSpace(r)
	})
}

func (c *Client) telegramLink(
	ctx context.Context,
	credential Credential,
	userID string,
	expected idpLink,
) (bool, error) {
	var response struct {
		Result  []idpLink `json:"result"`
		Details *struct {
			Total json.Number `json:"totalResult"`
		} `json:"details"`
	}
	err := c.jsonRequest(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(userID)+"/links/_search",
		map[string]any{"query": map[string]any{"limit": maxIdentityLinks}}, credential, &response)
	if err != nil {
		return false, err
	}
	if response.Details == nil {
		return false, ErrUnavailable
	}
	var total int64
	if response.Details.Total != "" {
		total, err = response.Details.Total.Int64()
	}
	if err != nil || total < 0 || total > maxIdentityLinks || int(total) != len(response.Result) {
		// Never infer absence or accept a matching row from a partial link listing.
		return false, ErrUnavailable
	}
	found := false
	for _, link := range response.Result {
		if link.Provider != expected.Provider {
			continue
		}
		if link.Subject != expected.Subject || found {
			return false, ErrConflict
		}
		found = true
	}
	return found, nil
}

func (c *Client) addTelegramLink(ctx context.Context, credential Credential, userID string, expected idpLink) error {
	var response struct{}
	writeErr := c.jsonRequest(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(userID)+"/links",
		map[string]any{"idpLink": expected}, credential, &response)
	if writeErr != nil && !errors.Is(writeErr, ErrConflict) && !errors.Is(writeErr, ErrUnavailable) {
		return writeErr
	}
	found, err := c.telegramLink(ctx, credential, userID, expected)
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	if writeErr != nil {
		return writeErr
	}
	return ErrUnavailable
}
