package telegram_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestDeliveryCooldownClassification(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, raw string
		kind      delivery.Kind
		missing   bool
	}{
		{"missing", `{}`, delivery.Deferred, true},
		{"zero", `{"retry_after":0}`, delivery.Deferred, false},
		{"large", `{"retry_after":20000000000}`, delivery.Deferred, false},
		{"negative", `{"retry_after":-1}`, delivery.Parked, false},
		{"overflow", `{"retry_after":9223372036854775808}`, delivery.Parked, false},
		{"string", `{"retry_after":"3"}`, delivery.Parked, false},
		{"null", `{"retry_after":null}`, delivery.Parked, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var parameters telegram.ResponseParameters
			require.NoError(t, json.Unmarshal([]byte(test.raw), &parameters))
			result := telegram.DeliveryOutcome(0, &telegram.APIError{Code: 429, Parameters: parameters})
			assert.Equal(t, test.kind, result.Kind)
			assert.Equal(t, test.missing, result.Missing)
			assert.True(t, result.Valid())
		})
	}
}

func TestDeliveryAmbiguousAndSharedFailures(t *testing.T) {
	t.Parallel()
	assert.Equal(t, delivery.Uncertain, telegram.DeliveryOutcome(0, context.Canceled).Kind)
	assert.Equal(t, delivery.Uncertain, telegram.DeliveryOutcome(0, context.DeadlineExceeded).Kind)
	assert.Equal(t, delivery.Uncertain, telegram.DeliveryOutcome(0, nil).Kind)
	assert.Equal(t, delivery.Paused, telegram.DeliveryOutcome(0, &telegram.APIError{Code: 401}).Kind)
	assert.Equal(t, delivery.Rejected, telegram.DeliveryOutcome(0, &telegram.APIError{Code: 403}).Kind)
	assert.Equal(t, delivery.Succeeded, telegram.DeliveryOutcome(123, nil).Kind)
}
