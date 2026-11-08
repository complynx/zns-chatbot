package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
)

const initialLoomRole = "zinusya-user"

// ErrInitialGrantRepair requires operator repair of an existing grant. The
// v4.20 update API replaces role keys without CAS; bootstrap never uses it.
var ErrInitialGrantRepair = fmt.Errorf("initial Loom grant requires operator repair: %w", ErrConflict)

type loomUserGrant struct {
	ID             string   `json:"id"`
	UserID         string   `json:"userId"`
	ProjectID      string   `json:"projectId"`
	ProjectGrantID string   `json:"projectGrantId"`
	OrgID          string   `json:"orgId"`
	State          string   `json:"state"`
	RoleKeys       []string `json:"roleKeys"`
	Details        struct {
		Org string `json:"resourceOwner"`
	} `json:"details"`
}

// Bootstrap owns exactly one initial role on the configured Loom project. It
// runs before the caller's durable binding, never during token/session refresh.
func (c *Client) ensureInitialLoomGrant(ctx context.Context, account Account) error {
	if account.Subject == "" || account.Subject == c.options.ActorID ||
		account.OrganizationID != c.options.OrganizationID || account.SyntheticEmail == "" {
		return ErrDenied
	}
	credential, err := c.ServiceToken(ctx)
	if err != nil {
		return err
	}
	if _, err = c.getAccount(ctx, credential, account.Subject, account.SyntheticEmail); err != nil {
		return err
	}
	// An earlier credential may predate the grant. Fence in-flight issuance and
	// discard cached role claims before returning to the local binding caller.
	c.Invalidate(account.Subject)
	defer c.Invalidate(account.Subject)
	found, err := c.initialLoomGrant(ctx, credential, account.Subject)
	if err != nil || found {
		return err
	}
	var response struct {
		ID string `json:"userGrantId"`
	}
	createErr := c.jsonRequest(ctx, http.MethodPost,
		"/management/v1/users/"+url.PathEscape(account.Subject)+"/grants",
		map[string]any{"projectId": c.options.LoomAudience, "roleKeys": []string{initialLoomRole}}, credential, &response)
	if createErr != nil && !errors.Is(createErr, ErrConflict) && !errors.Is(createErr, ErrUnavailable) {
		return createErr
	}
	// Neither success nor conflict proves the exact grant exists. Lost replies
	// use the same user/project tuple and reconcile through authoritative reads.
	found, err = c.initialLoomGrant(ctx, credential, account.Subject)
	if err != nil {
		return err
	}
	if !found {
		return ErrUnavailable
	}
	_, err = c.getAccount(ctx, credential, account.Subject, account.SyntheticEmail)
	return err
}

// Zitadel v4.20 management.proto ListUserGrants/AddUserGrant and user.proto
// UserGrantQuery define these routes and fields. Never search by the base role:
// doing so could hide an existing grant whose other roles must be preserved.
func (c *Client) initialLoomGrant(ctx context.Context, credential Credential, subject string) (bool, error) {
	var response struct {
		Result  []loomUserGrant `json:"result"`
		Details *struct {
			Total json.Number `json:"totalResult"`
		} `json:"details"`
	}
	body := struct {
		Query struct {
			Limit int `json:"limit"`
		} `json:"query"`
		Queries []any `json:"queries"`
	}{Queries: []any{
		map[string]any{"userIdQuery": map[string]any{"userId": subject}},
		map[string]any{"projectIdQuery": map[string]any{"projectId": c.options.LoomAudience}},
	}}
	body.Query.Limit = identitySearchLimit
	err := c.jsonRequest(ctx, http.MethodPost, "/management/v1/users/grants/_search", body, credential, &response)
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
	if err != nil || total < 0 || total != int64(len(response.Result)) {
		return false, ErrUnavailable
	}
	if total > 1 {
		return false, ErrConflict
	}
	if total == 0 {
		return false, nil
	}
	grant := response.Result[0]
	if grant.ID == "" || grant.UserID != subject || grant.ProjectID != c.options.LoomAudience ||
		grant.ProjectGrantID != "" || grant.OrgID != c.options.OrganizationID ||
		grant.Details.Org != c.options.OrganizationID {
		return false, ErrConflict
	}
	if grant.State != "USER_GRANT_STATE_ACTIVE" {
		return false, ErrDenied
	}
	if !slices.Contains(grant.RoleKeys, initialLoomRole) {
		return false, ErrInitialGrantRepair
	}
	return true, nil
}
