package agent_test

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestPaymentInstructionsClarifiesMultipleOrdersInPreferredLanguage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ language, want string }{
		{"en", "Specify the order ID"}, {"pl", "Specify the order ID"},
		{"ru", "Укажите ID заказа"}, {"by", "Укажите ID заказа"}, {"ua", "Укажите ID заказа"},
	} {
		t.Run(test.language, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer((&agent.ScriptedServer{}).Handler())
			t.Cleanup(server.Close)
			model := agent.Remote{URL: server.URL, HTTP: server.Client()}
			input := agent.Input{Language: test.language, Text: "payment instructions", OrderCount: 2,
				Business: &agent.BusinessCapabilities{CanBook: true},
				Orders:   []agent.OrderSummary{{ID: "first"}, {ID: "second"}}}
			plan, err := model.Plan(t.Context(), input)
			require.NoError(t, err)
			assert.Nil(t, plan.OrderAction)
			assert.Contains(t, plan.Text, test.want)
			input.Text += " second"
			plan, err = model.Plan(t.Context(), input)
			require.NoError(t, err)
			assert.Equal(t, &agent.OrderProposal{Name: "payment_instructions", OrderID: "second"}, plan.OrderAction)
		})
	}
}
