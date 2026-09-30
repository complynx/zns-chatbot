package integration_test

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func cashOrder(t *testing.T, f *fixture, key string) (orders.Order, orders.Command) {
	t.Helper()
	created, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Key:     key,
			Origin:  "manual",
			Choice:  orderChoice("preparty"),
		},
	)
	require.NoError(t, err)
	cash := orderCommand("cash", created)
	cash.PaymentAdmin = "bob"
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", cash)
	require.NoError(t, err)
	return order, cash
}

func TestNotificationServiceIdentityCannotActAsUser(t *testing.T) {
	t.Parallel()
	f := setup(t)
	for _, test := range []struct {
		path, token string
		status      int
	}{
		{path: "/internal/notifications", token: f.b.Host.Signer.Token("bob"), status: http.StatusUnauthorized},
		{path: "/v1/order-events/sandbox-festival/orders", token: f.b.Host.Signer.DeliveryToken(), status: http.StatusUnauthorized},
		{path: "/internal/notifications", token: f.b.Host.Signer.DeliveryToken(), status: http.StatusOK},
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.b.API.Base+test.path, http.NoBody)
		require.NoError(t, err)
		request.Header.Set("Authorization", "Bearer "+test.token)
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		assert.Equal(t, test.status, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
}

func TestPaymentNotificationsAreTransactionalAndReachUnopenedAdmin(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, cash := cashOrder(t, f, "create")
	_, err := f.b.API.ExecuteOrder(t.Context(), "alice", cash)
	require.NoError(t, err)
	stale := cash
	stale.Key = "stale"
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", stale)
	requireCode(t, err, "stale_version")
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_notifications`).Scan(&count))
	assert.Equal(t, 1, count)
	require.Empty(t, chatMessages(t, f, 202))
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	deliverNotificationBotCards(t, f)
	before := chatMessages(t, f, 202)
	require.NotEmpty(t, before, "admin receives a card without ever opening /orders")
	assertDeliveredOrderNotice(t, f, "bob", "payment_request", 202, "Ожидает проверки оплаты")
	assert.Contains(t, before[len(before)-1].Text, "Ожидается подтверждение наличных")
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	assert.Equal(t, before, chatMessages(t, f, 202), "completed queue record cannot send twice")
	reject := orderCommand("reject", order)
	_, err = f.b.API.ExecuteOrder(t.Context(), "bob", reject)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	deliverNotificationBotCards(t, f)
	messages := chatMessages(t, f, 101)
	require.NotEmpty(t, messages)
	assertDeliveredOrderNotice(t, f, "alice", "reject", 101, "Оплата отклонена")
	assert.Contains(t, messages[len(messages)-1].Text, "Не оплачен")
	handle(t, f.b, message(1000, 101, "почему изменилась оплата"))
	found := false
	for _, event := range f.model.input.History {
		if event.Kind == "order_notification" {
			found = true
			assert.Contains(t, string(event.Content), "system")
		}
	}
	assert.True(t, found, "agent sees the delivered system notice")
}

func TestBlockedRecipientDoesNotStopOtherDelivery(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, _ = cashOrder(t, f, "first")
	other, _ := cashOrder(t, f, "second")
	_, err := f.b.API.ExecuteOrder(t.Context(), "bob", orderCommand("accept", other))
	require.NoError(t, err)
	post(t, f.fake.URL+"/lab/blocked", map[string]any{"user": 202, "blocked": true})
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	var failure string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT failure FROM core.order_notifications WHERE recipient='bob' ORDER BY id LIMIT 1`).
			Scan(&failure),
	)
	assert.Equal(t, "telegram_recipient_rejected", failure)
	deliverNotificationBotCards(t, f)
	messages := chatMessages(t, f, 101)
	require.NotEmpty(t, messages)
	assertDeliveredOrderNotice(t, f, "alice", "accept", 101, "Оплата подтверждена")
	assert.Contains(t, messages[len(messages)-1].Text, "Статус: Оплачен")
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	notices, err := f.b.Host.PendingNotifications(t.Context())
	require.NoError(t, err)
	assert.Empty(t, notices, "superseded request is acknowledged without stale actions")
}

// The canonical receipt identifies the primary notice separately from its refreshed card.
func assertDeliveredOrderNotice(t *testing.T, f *fixture, owner, kind string, chat int64, text string) {
	t.Helper()
	var messageID, receiptID int64
	var state string
	var followupPending bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT n.telegram_message_id,d.message_id,n.delivery_state,n.followup_pending
 FROM core.order_notifications n JOIN bot.notification_deliveries d ON d.id=n.id
 WHERE n.recipient=$1 AND n.payload->>'kind'=$2 ORDER BY n.id DESC LIMIT 1`, owner, kind).
			Scan(&messageID, &receiptID, &state, &followupPending),
	)
	require.Positive(t, messageID)
	assert.Equal(t, messageID, receiptID)
	assert.Equal(t, "sent", state)
	assert.False(t, followupPending)
	messages := chatMessages(t, f, chat)
	found := 0
	for _, message := range messages {
		if message.ID == messageID {
			found++
			assert.Contains(t, message.Text, text)
		}
	}
	assert.Equal(t, 1, found, "the known primary notification remains visible exactly once")
	require.NotEmpty(t, messages)
	assert.NotEqual(t, messageID, messages[len(messages)-1].ID, "the current card is a separate follow-up")
}

type failAcknowledgment struct{ failed atomic.Bool }

func (transport *failAcknowledgment) RoundTrip(request *http.Request) (*http.Response, error) {
	if strings.HasSuffix(request.URL.Path, "/complete") && transport.failed.CompareAndSwap(false, true) {
		return nil, errors.New("simulated lost acknowledgment")
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestNotificationLostCanonicalAckDoesNotBlindResend(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, _ = cashOrder(t, f, "create")
	f.b.Host.HTTP = &http.Client{Transport: &failAcknowledgment{}}
	require.Error(t, f.b.DeliverNotifications(t.Context()))
	deliverNotificationBotCards(t, f)
	before := chatMessages(t, f, 202)
	require.NotEmpty(t, before, "the wire succeeded before the synthetic completion transport failure")
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.order_notifications SET lease_until=clock_timestamp()-interval '1 second' WHERE delivery_state='sending'`,
	)
	require.NoError(t, err)
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	assert.Equal(t, before, chatMessages(t, f, 202))
	var state, failure string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT delivery_state,failure FROM core.order_notifications ORDER BY id LIMIT 1`).
			Scan(&state, &failure),
	)
	assert.Equal(t, "unknown", state)
	assert.Equal(t, "telegram_outcome_unknown", failure)
}
func TestOrderFixturesEnableDeadlineAndRURouting(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, _ := cashOrder(t, f, "create")
	_, err := f.b.API.ExecuteOrder(t.Context(), "bob", orderCommand("reject", order))
	require.NoError(t, err)
	order, err = f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	proof := orderCommand("proof", order)
	proof.ProofFile = uploadProof(t, orders.Service{DB: f.db}, "alice")
	order, err = f.b.API.ExecuteOrder(t.Context(), "alice", proof)
	require.NoError(t, err)
	require.NoError(
		t,
		sandbox.ApplyOrderFixture(
			t.Context(),
			f.db,
			sandbox.OrderFixture{
				Deadline:     time.Now().Add(-time.Hour),
				OrderID:      order.ID,
				Age:          72 * time.Hour,
				AdminCountry: "ru",
			},
		),
	)
	country := orderCommand("country", order)
	country.Country, country.PaymentAdmin = "ru", "bob"
	order, err = f.b.API.ExecuteOrder(t.Context(), "alice", country)
	require.NoError(t, err, "routing an already received proof remains possible after cutoff")
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orderCommand("cancel_proof", order))
	requireCode(t, err, "deadline")
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	require.NoError(t, f.b.DeliverNotifications(t.Context()))
	deliverNotificationBotCards(t, f)
	messages := chatMessages(t, f, 101)
	require.NotEmpty(t, messages)
	assert.Contains(t, messages[len(messages)-1].Text, "1\u00a0050,00 RUB")
	_, err = f.b.API.ExecuteOrder(t.Context(), "bob", orderCommand("accept", order))
	require.NoError(t, err)
	require.Error(
		t,
		sandbox.ApplyOrderFixture(t.Context(), f.db, sandbox.OrderFixture{OrderID: "missing", Age: time.Hour}),
	)
}
