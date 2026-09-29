package appclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PassTakeoverTarget(
	ctx context.Context,
	owner, event string,
	id int64,
) (passbooking.TakeoverTarget, error) {
	if c.LocalRegistration != nil {
		return c.localPassTakeoverTarget(ctx, owner, event, id)
	}
	var result passbooking.TakeoverTarget
	err := c.Call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/takeover/"+strconv.FormatInt(id, 10),
		nil,
		&result,
	)
	return registrationHTTPResult(result, err)
}
