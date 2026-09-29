package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) localPassEventsPage(
	ctx context.Context,
	owner, cursor string,
) (core.ReadPage[passbooking.NavigationEvent], error) {
	value, err := directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (core.ReadPage[passbooking.NavigationEvent], error) {
			return service.EventsPage(ctx, actor, cursor)
		},
	)
	// Position is domain cursor state, excluded from the public HTTP representation.
	for i := range value.Items {
		value.Items[i].Position = 0
	}
	return value, ReadError(err)
}

func (c Client) localPassEventDetail(ctx context.Context, owner, event, cursor string) (core.ReadChunk, error) {
	value, err := directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (core.ReadChunk, error) {
			return service.EventDetail(ctx, actor, event, cursor)
		},
	)
	return value, ReadError(err)
}

func (c Client) localPassPaymentHistory(
	ctx context.Context,
	owner, event, cursor string,
) (core.ReadPage[passbooking.PaymentHistoryEntry], error) {
	value, err := directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (core.ReadPage[passbooking.PaymentHistoryEntry], error) {
			return service.PaymentHistoryPage(ctx, actor, event, cursor)
		},
	)
	return value, ReadError(err)
}

func (c Client) localOwnsPassEvents(ctx context.Context, owner string, events []string) (bool, error) {
	return directRegistration(ctx, c, owner, func(service passbooking.Service, actor string) (bool, error) {
		return service.OwnsEventBookings(ctx, actor, events)
	})
}
