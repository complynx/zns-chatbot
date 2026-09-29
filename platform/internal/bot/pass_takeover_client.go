package bot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c APIClient) PassTakeoverTarget(
	ctx context.Context,
	owner, event string,
	id int64,
) (passbooking.TakeoverTarget, error) {
	var result passbooking.TakeoverTarget
	err := c.call(
		ctx,
		owner,
		http.MethodGet,
		"/v1/passes/events/"+url.PathEscape(event)+"/takeover/"+strconv.FormatInt(id, 10),
		nil,
		&result,
	)
	return result, err
}
