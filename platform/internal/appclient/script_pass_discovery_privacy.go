package appclient

import (
	"context"
	"net/http"
	"net/url"
)

func (c Client) OwnsPassEvents(ctx context.Context, owner string, events []string) (bool, error) {
	if c.LocalRegistration != nil {
		return c.localOwnsPassEvents(ctx, owner, events)
	}
	var owned bool
	query := url.Values{knowledgeEventQuery: events}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/passes/bookings/owned?"+query.Encode(), nil, &owned)
	return registrationHTTPResult(owned, err)
}
