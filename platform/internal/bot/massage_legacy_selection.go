package bot

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func (r *massageRenderer) readBookings(ctx context.Context) ([]massage.Reservation, error) {
	if r.state.View != massageLegacyClients {
		return r.bot.API.MassageBookings(ctx, r.owner, r.state.Event, "", r.state.View)
	}
	if r.state.LegacyBooking == "" {
		return r.bot.API.MassageBookings(ctx, r.owner, r.state.Event, r.state.Party, massageClients)
	}
	var booking massage.Reservation
	err := r.bot.API.call(
		ctx,
		r.owner,
		http.MethodGet,
		"/v1/massage/legacy-practitioner-booking?"+url.Values{
			knowledgeEventQuery: {r.state.Event},
			"id":                {r.state.LegacyBooking},
		}.Encode(),
		nil,
		&booking,
	)
	if err != nil {
		return nil, err
	}
	return []massage.Reservation{booking}, nil
}
