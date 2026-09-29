package passbooking_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestClosestEventUsesSaleStart(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	earlier, later, future := now.Add(-2*time.Hour), now.Add(-time.Hour), now.Add(time.Hour)
	events := []passbooking.Event{
		{ID: "earlier", SalesStart: &earlier, OpenEnded: true},
		{ID: "future", SalesStart: &future, OpenEnded: true},
		{ID: "later", SalesStart: &later, OpenEnded: true},
	}
	assert.Equal(t, "later", passbooking.ClosestEvent(events, now))
	assert.Equal(t, "earlier", passbooking.ClosestEvent(events, now.Add(-3*time.Hour)))
}
