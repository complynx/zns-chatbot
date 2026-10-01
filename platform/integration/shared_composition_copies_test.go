package integration_test

import (
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
)

func TestSyntheticCompositionBindsCopiedServicesBeforeConstruction(t *testing.T) {
	t.Parallel()
	// A nil pool proves assembly does not query or start work. These copied
	// services were missed when the fixture rewired only top-level fields.
	services := notificationFixtureServices(nil, appservices.Options{})
	t.Run("native_intake", func(t *testing.T) {
		t.Parallel()
		resolver, ok := services.Registration.Intake.(*derivedmutation.NativeRegistrationResolver)
		require.True(t, ok)
		require.Equal(t, syntheticDeliverySettings(), resolver.Service.Registration.Delivery)
		require.NoError(t, resolver.Service.Registration.Delivery.Validate())
	})
	t.Run("food_delivery", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, syntheticDeliverySettings(), services.BotDelivery.Food.Delivery)
		// Valid composition reaches request validation; a zero-settings copy fails
		// earlier with ErrSettings. An invalid attempt never accesses the nil pool.
		_, err := services.BotDelivery.Food.BeginNotification(t.Context(), legacyfood.NotificationAttempt{Attempt: delivery.Attempt{}, Wire: notificationTestWire()})
		var problem *core.ProblemError
		require.ErrorAs(t, err, &problem)
		require.Equal(t, http.StatusBadRequest, problem.Status)
	})
}
