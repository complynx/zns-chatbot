package bot

import (
	"context"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func (c APIClient) PassProfile(ctx context.Context, owner string) (passes.Profile, error) {
	var result passes.Profile
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/pass-profile", nil, &result)
	return result, err
}

func (c APIClient) PassProfileHistory(ctx context.Context, owner string) ([]passes.Change, error) {
	var result []passes.Change
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/pass-profile/history", nil, &result)
	return result, err
}

func (c APIClient) ExecutePassProfile(
	ctx context.Context,
	owner string,
	command passes.Command,
) (passes.Profile, error) {
	var result passes.Profile
	err := c.call(ctx, owner, http.MethodPost, "/v1/me/pass-profile/actions", command, &result)
	return result, err
}

func (c APIClient) PassProfileHistoryPage(ctx context.Context, owner string, before int64) (passes.HistoryPage, error) {
	var result passes.HistoryPage
	err := c.call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/me/pass-profile/history/page?before="+strconv.FormatInt(before, 10),
		nil,
		&result,
	)
	return result, err
}
