package task

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestReturnedRegistrationShowsOutstandingRefund(t *testing.T) {
	t.Parallel()
	settlement := &db.AccountPassReturn{Currency: "BYN", CurrencyScale: 2, RefundDueMinor: 2087}
	event := db.AccountEvent{Registration: db.AccountRegistration{State: db.PassReturned}, PassReturn: settlement}
	require.Contains(t, structuredEvent(event, false), passText(false, "ops.refundDue")+" · 20.87 BYN")
	settlement.RefundedMinor = settlement.RefundDueMinor
	result := structuredEvent(event, false)
	require.Contains(t, result, passText(false, "ops.refundDue")+" · 0.00 BYN")
	require.Contains(t, result, passText(false, "ops.refundedAmount")+" · 20.87 BYN")
}

func TestAssignmentNoticeUsesFrozenDepositAndFinalDeadlines(t *testing.T) {
	t.Parallel()
	initial := time.Date(2027, 1, 5, 17, 0, 0, 0, time.UTC)
	final := initial.AddDate(0, 0, 7)
	pass := registrations.Pass{Currency: "EUR", CurrencyScale: 2, PriceMinor: 10000, PaymentDueAt: initial,
		Deposit: &registrations.DepositSnapshot{RequiredMinor: 2500, FinalDueAt: final}}
	render := func() string { return passAssignmentText("en", "Dance", registrations.Follower, time.UTC, pass) }
	firstDate := initial.Format("2006-01-02")
	lastDate := final.Format("2006-01-02")
	partial := render()
	require.Contains(t, partial, "25.00 EUR")
	require.Contains(t, partial, "100.00 EUR")
	require.Contains(t, partial, firstDate)
	require.Contains(t, partial, lastDate)
	pass.Deposit.RequiredMinor = 0
	zero := render()
	require.NotContains(t, zero, firstDate)
	require.Contains(t, zero, lastDate)
	pass.Deposit.RequiredMinor = pass.PriceMinor
	full := render()
	require.Contains(t, full, firstDate)
	require.NotContains(t, full, lastDate)
}
