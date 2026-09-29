package appclient

import (
	"context"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func (c Client) PassProfile(ctx context.Context, owner string) (passes.Profile, error) {
	if c.LocalRegistration != nil {
		return c.localPassProfile(ctx, owner)
	}
	var result passes.Profile
	err := c.Call(ctx, owner, http.MethodGet, "/v1/me/pass-profile", nil, &result)
	return registrationHTTPResult(result, err)
}

func (c Client) PassProfileHistory(ctx context.Context, owner string) ([]passes.Change, error) {
	if c.LocalRegistration != nil {
		return c.localPassProfileHistory(ctx, owner)
	}
	var result []passes.Change
	err := c.Call(ctx, owner, http.MethodGet, "/v1/me/pass-profile/history", nil, &result)
	return registrationHTTPResult(result, err)
}

func (c Client) ExecutePassProfile(
	ctx context.Context,
	owner string,
	command passes.Command,
) (passes.Profile, error) {
	if c.LocalRegistration != nil {
		return c.localExecutePassProfile(ctx, owner, command)
	}
	var result passes.Profile
	err := c.Call(ctx, owner, http.MethodPost, "/v1/me/pass-profile/actions", command, &result)
	return registrationHTTPResult(result, err)
}

func (c Client) PassProfileHistoryPage(ctx context.Context, owner string, before int64) (passes.HistoryPage, error) {
	if c.LocalRegistration != nil {
		return c.localPassProfileHistoryPage(ctx, owner, before)
	}
	var result passes.HistoryPage
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/me/pass-profile/history/page?before="+strconv.FormatInt(before, 10),
		nil,
		&result,
	)
	return registrationHTTPResult(result, err)
}
