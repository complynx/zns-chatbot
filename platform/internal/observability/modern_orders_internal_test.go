package observability

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModernOrderDiagnosticNames(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"orders.events", "orders.event", "orders.browse", "orders.contacts", "orders.history.page", "orders.history.read",
		"orders.choice", "orders.inspect", "orders.instructions", "orders.proof", "orders.quote", "orders.update", "orders.inbox",
		"orders.review.read", "orders.review.decide", "orders.review.proof", "orders.export"} {
		assert.Equal(t, name, safeAgentEvent(AgentEvent{Operation: name}).Operation)
	}
	assert.Equal(t, "unknown", safeAgentEvent(AgentEvent{Operation: "orders.choice.private-customer-canary"}).Operation)
}
