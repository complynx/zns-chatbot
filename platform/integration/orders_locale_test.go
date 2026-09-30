package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func orderChatText(t *testing.T, f *fixture, chat int64) string {
	t.Helper()
	var text strings.Builder
	for _, message := range chatMessages(t, f, chat) {
		text.WriteString(message.Text + "\n")
		for _, row := range message.Markup.Rows {
			for _, button := range row {
				text.WriteString(button.Text + "\n")
			}
		}
	}
	return text.String()
}

func TestOrdersLocaleRefreshAndRecipientNotifications(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.b.API.SetLanguage(t.Context(), "alice", "ru", false)
	require.NoError(t, err)
	handleVisible(t, f.b, message(950, 101, "/orders"))
	created, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Key:     "localized",
			Origin:  "manual",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	handleVisible(t, f.b, message(951, 101, "/language"))
	handleVisible(t, f.b, orderClick(t, f, 101, 952, "en"))
	english := orderChatText(t, f, 101)
	for _, label := range []string{"Festival orders.", "New order", "Status: Unpaid", "Preparty", "Send receipt", "Delete order"} {
		assert.Contains(t, english, label)
	}
	assert.NotContains(t, english, "Заказы фестиваля")
	_, err = f.b.API.SetLanguage(t.Context(), "bob", "en", false)
	require.NoError(t, err)
	cash := orderCommand("cash", created)
	cash.PaymentAdmin = "bob"
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", cash)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	pumpBotDeliveries(t, f.b)
	admin := orderChatText(t, f, 202)
	assert.Contains(t, admin, "Awaiting payment review")
	assert.Contains(t, admin, "Accept payment")
	assert.NotContains(t, admin, "Проверка оплаты")
	_, err = f.b.API.SetLanguage(t.Context(), "alice", "ru", false)
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	russian := orderChatText(t, f, 101)
	assert.Contains(t, russian, "Заказы фестиваля")
	assert.Contains(t, russian, "Новый заказ")
	assert.Contains(t, russian, "Препати")
	assert.Contains(t, russian, "Запрошена оплата наличными")
	f.model.plan = agent.Plan{View: agent.OrdersView, Text: "Your order is awaiting review."}
	handleVisible(t, f.b, message(953, 101, "Please explain my order in English."))
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	assert.Contains(
		t,
		orderChatText(t, f, 101),
		"Your order is awaiting review.",
		"GUI locale must not translate the conversational answer",
	)
}
