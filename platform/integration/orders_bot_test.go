package integration_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func orderClick(t *testing.T, f *fixture, user, update int64, label string) telegram.Update {
	t.Helper()
	messages := chatMessages(t, f, user)
	for _, message := range messages {
		for _, row := range message.Markup.Rows {
			for _, button := range row {
				if button.Text == label {
					return telegram.Update{ID: update, Callback: &telegram.Callback{
						ID: strconv.FormatInt(
							update,
							10,
						),
						From:    telegram.User{ID: user},
						Data:    button.Data,
						Message: message,
					}}
				}
			}
		}
	}
	t.Fatalf("missing button %q for user %d", label, user)
	return telegram.Update{}
}

func chatMessages(t *testing.T, f *fixture, user int64) []telegram.Message {
	t.Helper()
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		f.fake.URL+"/lab/state?user="+strconv.FormatInt(user, 10),
		nil,
	)
	require.NoError(t, err)
	response, err := f.fake.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var state struct {
		Messages []telegram.Message `json:"messages"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&state))
	return state.Messages
}

func TestTelegramOrderCashLifecycleAndStaleCards(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(100, 101, "/orders"))
	create := orderClick(t, f, 101, 101, "Новый заказ")
	handleVisible(t, f.b, create)
	handleVisible(t, f.b, create)
	service := orders.Service{DB: f.db}
	list, err := service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 1, "redelivered Telegram update cannot create twice")
	stale := orderClick(t, f, 101, 104, "Добавить: Препати")
	handleVisible(t, f.b, orderClick(t, f, 101, 102, "Добавить: Трансфер"))
	before := chatMessages(t, f, 101)
	handleVisible(t, f.b, stale)
	after := chatMessages(t, f, 101)
	require.Len(t, after, len(before), "stale callback refreshes existing cards")
	require.GreaterOrEqual(t, len(after), 2)
	assert.Contains(t, after[0].Text, "stale_version")
	assert.Contains(t, after[1].Text, "65,00 BYN")
	handleVisible(t, f.b, orderClick(t, f, 101, 105, "Наличные: Борис"))
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	pumpBotDeliveries(t, f.b)
	handleVisible(t, f.b, message(106, 202, "/orders"))
	accept := orderClick(t, f, 202, 107, "Принять оплату")
	handleVisible(t, f.b, accept)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	pumpBotDeliveries(t, f.b)
	list, err = service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "paid", list[0].State)
	assert.Equal(t, orders.Money(6500), list[0].Choice.Total)
	after = chatMessages(t, f, 101)
	require.GreaterOrEqual(t, len(after), 2)
	assert.Contains(t, after[1].Text, "Статус: Оплачен")
	assert.Contains(t, after[1].Text, "Трансфер: 65,00 BYN")
	assert.Empty(t, after[1].Markup.Rows)
	for _, msg := range chatMessages(t, f, 202) {
		assert.NotContains(t, msg.Text, "Проверка оплаты")
	}
}

func TestTelegramOrderButtonsAreOwnerScoped(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(100, 101, "/orders"))
	foreign := orderClick(t, f, 101, 101, "Новый заказ")
	foreign.Callback.From.ID = 303
	foreign.Callback.Message.Chat.ID = 303
	handleVisible(t, f.b, foreign)
	messages := chatMessages(t, f, 303)
	require.NotEmpty(t, messages)
	assert.Contains(t, messages[0].Text, "Кнопка недоступна")
	handleVisible(t, f.b, orderClick(t, f, 303, 102, "Новый заказ"))
	messages = chatMessages(t, f, 303)
	require.NotEmpty(t, messages)
	assert.Contains(t, messages[0].Text, "forbidden")
	for _, msg := range chatMessages(t, f, 303) {
		assert.NotContains(t, msg.Text, "Проверка оплаты")
	}
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders`).Scan(&count))
	assert.Zero(t, count)
}

func TestAgentContinuesManualOrderAndBindsVersionAcrossRetries(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(100, 101, "/orders"))
	handleVisible(t, f.b, orderClick(t, f, 101, 101, "Новый заказ"))
	handleVisible(t, f.b, orderClick(t, f, 101, 102, "Добавить: Трансфер"))
	stale := orderClick(t, f, 101, 104, "Добавить: Препати")
	service := orders.Service{DB: f.db}
	list, err := service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 1)
	f.model.plan = agent.Plan{
		View:        "orders",
		OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: list[0].ID, Extra: "preparty"},
	}
	request := message(103, 101, "добавь препати в заказ")
	handleVisible(t, f.b, request)
	assert.Equal(t, "orders", f.model.input.View)
	require.Len(t, f.model.input.Orders, 1)
	assert.Equal(t, orders.Money(6500), f.model.input.Orders[0].Choice.Total)
	assert.NotEmpty(t, f.model.input.History)
	handleVisible(t, f.b, request)
	list, err = service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	assert.EqualValues(t, 3, list[0].Version)
	assert.Equal(t, orders.Money(10000), list[0].Choice.Total)
	handleVisible(t, f.b, stale)
	messages := chatMessages(t, f, 101)
	require.GreaterOrEqual(t, len(messages), 2)
	assert.Contains(t, messages[0].Text, "stale_version")
	assert.Contains(t, messages[1].Text, "100,00 BYN")
	f.model.plan = agent.Plan{View: "orders", OrderAction: &agent.OrderProposal{Name: "create"}}
	handleVisible(t, f.b, message(105, 303, "создай заказ"))
	messages = chatMessages(t, f, 303)
	require.NotEmpty(t, messages)
	assert.Contains(t, messages[0].Text, "forbidden")
	f.model.plan = agent.Plan{
		View:        "orders",
		OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: list[0].ID, Extra: "shuttle"},
	}
	handleVisible(t, f.b, message(106, 202, "измени чужой заказ"))
	list, err = service.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	assert.EqualValues(t, 3, list[0].Version, "foreign model proposal cannot modify Alice's order")
}

func TestAgentReceivesRemovedManualOrderChoices(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handleVisible(t, f.b, message(100, 101, "/orders"))
	handleVisible(t, f.b, orderClick(t, f, 101, 101, "Новый заказ"))
	handleVisible(t, f.b, orderClick(t, f, 101, 102, "Добавить: Трансфер"))
	handleVisible(t, f.b, orderClick(t, f, 101, 103, "Убрать: Трансфер"))
	f.model.plan = agent.Plan{View: "orders", Text: "Вижу историю заказа."}
	handle(t, f.b, message(104, 101, "Что я только что убрал?"))
	changes := f.model.input.OrderHistory
	require.Len(t, changes, 3)
	assert.Equal(t, changes[0].OrderID, changes[1].OrderID)
	assert.Equal(t, changes[1].OrderID, changes[2].OrderID)
	assert.Equal(t, "manual", changes[2].Origin)
	assert.EqualValues(t, 2, changes[1].Version)
	assert.EqualValues(t, 3, changes[2].Version)
	assert.Equal(t, orders.Money(6500), changes[1].Extras["shuttle"])
	assert.Equal(t, orders.Money(6500), changes[1].Total)
	assert.NotContains(t, changes[2].Extras, "shuttle")
	assert.Zero(t, changes[2].Total)
}
