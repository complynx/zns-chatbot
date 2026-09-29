package massage

import "context"

type CalendarClient struct {
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	TelegramID int64  `json:"telegram_id"`
}

type Calendar struct {
	Parties   []EventParty     `json:"parties"`
	Providers []Provider       `json:"providers"`
	Bookings  []Reservation    `json:"bookings"`
	Clients   []CalendarClient `json:"clients"`
}

// Timetable includes client identity only after specialist/admin authorization.
func (s Service) Timetable(ctx context.Context, actor, event string) (Calendar, error) {
	bookings, err := s.Bookings(ctx, actor, event, "", "timetable")
	if err != nil {
		return Calendar{}, err
	}
	parties, err := s.Parties(ctx, actor, event)
	if err != nil {
		return Calendar{}, err
	}
	staff, err := providers(ctx, s.DB, event)
	if err != nil {
		return Calendar{}, err
	}
	result := Calendar{Bookings: bookings, Providers: staff, Parties: []EventParty{}, Clients: []CalendarClient{}}
	for _, party := range parties {
		if !party.Open {
			result.Parties = append(result.Parties, party)
		}
	}
	seen := map[string]bool{}
	for _, booking := range bookings {
		if seen[booking.Owner] {
			continue
		}
		var client CalendarClient
		err = s.DB.QueryRow(ctx, `SELECT id,name,telegram_id FROM core.users WHERE id=$1`, booking.Owner).
			Scan(&client.Owner, &client.Name, &client.TelegramID)
		if err != nil {
			return Calendar{}, err
		}
		seen[booking.Owner] = true
		result.Clients = append(result.Clients, client)
	}
	return result, nil
}
