package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func (c Client) MassageTimetable(ctx context.Context, owner, event string) (massage.Calendar, error) {
	var result massage.Calendar
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/timetable?event="+url.QueryEscape(event), nil, &result)
	return result, err
}
