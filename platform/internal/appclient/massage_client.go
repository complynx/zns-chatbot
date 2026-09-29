package appclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func (c Client) MassageParties(ctx context.Context, owner, event string) ([]massage.EventParty, error) {
	var result []massage.EventParty
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/parties?event="+url.QueryEscape(event), nil, &result)
	return result, err
}

func (c Client) MassageProviderNames(ctx context.Context, owner, event string) (map[string]string, error) {
	var result map[string]string
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/provider-names?event="+url.QueryEscape(event), nil, &result)
	return result, err
}

func (c Client) MassageSlots(
	ctx context.Context,
	owner, event, party string,
	length int,
) (massage.Availability, error) {
	var result massage.Availability
	query := url.Values{knowledgeEventQuery: {event}, "party": {party}, massageLength: {strconv.Itoa(length)}}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/slots?"+query.Encode(), nil, &result)
	return result, err
}

func (c Client) MassageBookings(
	ctx context.Context,
	owner, event, party, view string,
) ([]massage.Reservation, error) {
	var result []massage.Reservation
	query := url.Values{knowledgeEventQuery: {event}, "party": {party}, "view": {view}}
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/bookings?"+query.Encode(), nil, &result)
	return result, err
}

func (c Client) MassagePreferences(ctx context.Context, owner, event string) (massage.Preferences, error) {
	var result massage.Preferences
	err := c.Call(ctx, owner, http.MethodGet, "/v1/massage/preferences?event="+url.QueryEscape(event), nil, &result)
	return result, err
}

func (c Client) SetMassagePreferences(
	ctx context.Context,
	owner, event string,
	prefs massage.Preferences,
) (massage.Preferences, error) {
	var result massage.Preferences
	err := c.Call(ctx, owner, http.MethodPut, "/v1/massage/preferences?event="+url.QueryEscape(event), prefs, &result)
	return result, err
}

func (c Client) ExecuteMassage(
	ctx context.Context,
	owner string,
	command massage.Command,
) (massage.Reservation, error) {
	var result massage.Reservation
	err := c.Call(ctx, owner, http.MethodPost, "/v1/massage/actions", command, &result)
	return result, err
}
