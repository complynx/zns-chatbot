package passbooking

import (
	"context"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

// OwnsEventBookings checks one discovery page without exposing other owners or
// booking contents. Repeated IDs have the same meaning as a single ID.
func (s Service) OwnsEventBookings(ctx context.Context, actor string, events []string) (bool, error) {
	if len(events) == 0 || len(events) > core.ReadPageItems {
		return false, invalid()
	}
	if slices.Contains(events, "") {
		return false, invalid()
	}
	if err := s.requireActor(ctx, actor); err != nil {
		return false, err
	}
	return dbgen.New(s.DB).OwnsEventBookings(ctx, dbgen.OwnsEventBookingsParams{Events: events, Owner: actor})
}
