package appclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PassAdminTarget(
	ctx context.Context,
	actor, event string,
	telegramID int64,
) (passbooking.AdminTarget, error) {
	if c.LocalRegistration != nil {
		return c.localPassAdminTarget(ctx, actor, event, telegramID)
	}
	var result passbooking.AdminTarget
	err := c.Call(
		ctx,
		actor,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/admin/targets/"+strconv.FormatInt(telegramID, 10),
		nil,
		&result,
	)
	return registrationHTTPResult(result, err)
}

func (c Client) AssignPass(
	ctx context.Context,
	actor string,
	command passbooking.AdminAssignment,
) (passbooking.AdminAssignmentResult, error) {
	if c.LocalRegistration != nil {
		return c.localAssignPass(ctx, actor, command)
	}
	var result passbooking.AdminAssignmentResult
	err := c.Call(ctx, actor, http.MethodPost, "/v1/passes/admin/assign", command, &result)
	return registrationHTTPResult(result, err)
}
