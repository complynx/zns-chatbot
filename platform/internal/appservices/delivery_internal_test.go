package appservices

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
)

type destinationResolverStub struct{ calls int }

func (r *destinationResolverStub) ResolveChat(context.Context, string) (int64, error) {
	r.calls++
	return 77, nil
}

func TestDeliverySettingsReachEveryOutboxOwner(t *testing.T) {
	t.Parallel()
	settings := delivery.Settings{
		BotID: 8123, BotInterval: 50 * time.Millisecond,
		ChatInterval: time.Second, Fallback: 30 * time.Second,
	}
	require.NoError(t, settings.Validate())
	bindings := &destination.Bindings{}
	resolver := &destinationResolverStub{}
	services := NewServices(nil, Options{
		LegacyOrderBotID: settings.BotID, Delivery: settings,
		AnnouncementBindings: bindings, DestinationResolver: resolver,
	})
	assert.Equal(t, settings, services.Registration.Delivery)
	assert.Equal(t, settings, services.DerivedMutations.Registration.Delivery)
	assert.Equal(t, settings, services.AdminMessages.Delivery)
	assert.Equal(t, settings, services.AdminUtilities.Registration.Delivery)
	assert.Equal(t, settings.BotID, services.AdminMessages.BotID)
	assert.Equal(t, settings, services.Orders.Delivery)
	assert.Equal(t, settings, services.LegacyOrders.Delivery)
	assert.Equal(t, settings, services.Massage.Delivery)
	assert.Equal(t, settings, services.LegacyFood.Delivery)
	assert.Equal(t, settings, services.DerivedMutations.Orders.Delivery)
	assert.Equal(t, settings, services.DerivedMutations.Massage.Delivery)
	assert.Equal(t, settings, services.DerivedMutations.Food.Delivery)
	assert.Same(t, bindings, services.Registration.AnnouncementBindings)
	assert.Same(t, bindings, services.DerivedMutations.Registration.AnnouncementBindings)
	assert.Same(t, bindings, services.AdminUtilities.Registration.AnnouncementBindings)
	assert.Same(t, resolver, services.AdminMessages.DestinationResolver)
	assert.Zero(t, resolver.calls, "service construction must not start provider work")
}
