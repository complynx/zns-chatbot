package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestPaymentInstructionsReadOnlyAndOwnerScoped(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "instructions",
		Choice:  orderChoice("preparty"),
	})
	require.NoError(t, err)
	info, err := f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, orders.Money(3500), info.TotalBYN)
	assert.Equal(t, "1050.00", info.TotalRUB)
	assert.True(t, info.CanPay)
	assert.Contains(t, info.Transfer, "ТЕСТОВЫЕ")
	require.Len(t, info.Contacts, 1)
	assert.EqualValues(t, 202, info.Contacts[0].TelegramID)
	current, err := f.b.API.Order(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.Equal(t, order.Version, current.Version)
	_, err = f.b.API.PaymentInstructions(t.Context(), "bob", order.EventID, order.ID)
	require.Error(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.order_events SET deadline=now()-interval '1 day' WHERE id=$1`,
		order.EventID,
	)
	require.NoError(t, err)
	info, err = f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
	require.NoError(t, err)
	assert.False(t, info.CanPay)
	assert.Empty(t, info.Transfer)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	_, err = f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
	require.Error(t, err)
}

func TestPaymentInstructionsLocalizedContentFallback(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ tag, want string }{
		{"by", "Russian instructions"}, {"ua", "Russian instructions"},
		{"be-BY", "Russian instructions"}, {"uk-UA", "Exact Ukrainian instructions"},
		{"pl", "English instructions"}, {"pl-PL", "English instructions"},
	} {
		t.Run(test.tag, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
				EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "fallback",
				Choice: orderChoice("preparty"),
			})
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET transfer_instructions_localized=
				'{"ru":"Russian instructions","en":"English instructions","uk-UA":"Exact Ukrainian instructions"}' WHERE id=$1`, order.EventID)
			require.NoError(t, err)
			_, resetErr := f.db.Exec(t.Context(), `UPDATE core.users SET language='' WHERE id='alice'`)
			require.NoError(t, resetErr)
			_, setErr := f.b.API.SetLanguage(t.Context(), "alice", test.tag, true)
			require.NoError(t, setErr)
			info, readErr := f.b.API.PaymentInstructions(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, readErr)
			assert.Equal(t, test.want, info.Transfer)
		})
	}
}

func TestDeletedPaymentCardKeepsSelectedLanguage(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order, err := f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "retire",
		Choice: orderChoice("preparty"),
	})
	require.NoError(t, err)
	handleVisible(t, f.b, message(900, 101, "/language en"))
	handleVisible(t, f.b, message(901, 101, "/orders"))
	handleVisible(t, f.b, orderClick(t, f, 101, 902, "Payment methods"))
	opened := paymentMessage(t, f)
	_, err = f.b.API.ExecuteOrder(t.Context(), "alice", orders.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "delete", Origin: "manual", Key: "delete-retired",
	})
	require.NoError(t, err)
	require.NoError(t, f.b.RenderOrders(t.Context(), "alice", 101))
	pumpBotDeliveries(t, f.b)
	var found bool
	for _, item := range chatMessages(t, f, 101) {
		if item.ID == opened.ID {
			found = true
			assert.Contains(t, item.Text, "Payment instructions are unavailable")
			assert.NotContains(t, item.Text, "TEST PAYMENT DETAILS")
			assert.Empty(t, item.Markup.Rows)
		}
	}
	assert.True(t, found)
}
