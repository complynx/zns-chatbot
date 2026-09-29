package bot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c APIClient) PassAdminTarget(
	ctx context.Context,
	actor, event string,
	telegramID int64,
) (passbooking.AdminTarget, error) {
	var result passbooking.AdminTarget
	err := c.call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/admin/targets/"+strconv.FormatInt(telegramID, 10),
		nil,
		&result,
	)
	return result, err
}

func (c APIClient) AssignPass(
	ctx context.Context,
	actor string,
	command passbooking.AdminAssignment,
) (passbooking.AdminAssignmentResult, error) {
	var result passbooking.AdminAssignmentResult
	err := c.call(ctx, actor, http.MethodPost, "/v1/passes/admin/assign", command, &result)
	return result, err
}
