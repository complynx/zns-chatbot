package payments_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestConfirmedDepositsAndTopUpsUseExactFrozenPrice(t *testing.T) {
	t.Parallel()
	pass := registrations.Pass{
		PriceMinor:    12345,
		Currency:      "BYN",
		CurrencyScale: 2,
		Deposit:       &registrations.DepositSnapshot{RequiredMinor: 3087},
	}
	for _, tc := range []struct {
		total, remaining int64
		deposit, paid    bool
	}{{0, 12345, false, false}, {1000, 11345, false, false}, {3087, 9258, true, false}, {12345, 0, true, true}} {
		balance, err := payments.Summarize(pass, 2, tc.total)
		require.NoError(t, err)
		require.Equal(t, tc.remaining, balance.RemainingMinor)
		require.Equal(t, tc.deposit, balance.DepositSatisfied)
		require.Equal(t, tc.paid, balance.FullyPaid)
	}
	pass.PriceMinor = payments.MaxAmount
	pass.Deposit.RequiredMinor = payments.MaxAmount
	balance, err := payments.Summarize(pass, 1, payments.MaxAmount-1)
	require.NoError(t, err)
	require.EqualValues(t, 1, balance.RemainingMinor)
	require.False(t, balance.FullyPaid)
	_, err = payments.Summarize(pass, 1, payments.MaxAmount+1)
	require.ErrorIs(t, err, payments.ErrInvalid)
}
func TestZeroDepositDoesNotInventPaymentAndFreePassNeedsNone(t *testing.T) {
	t.Parallel()
	pass := registrations.Pass{PriceMinor: 1000, Deposit: &registrations.DepositSnapshot{RequiredMinor: 0}}
	balance, err := payments.Summarize(pass, 0, 0)
	require.NoError(t, err)
	require.False(t, balance.DepositSatisfied)
	require.False(t, balance.FullyPaid)
	balance, err = payments.Summarize(pass, 1, 1)
	require.NoError(t, err)
	require.True(t, balance.DepositSatisfied)
	require.False(t, balance.FullyPaid)
	pass.PriceMinor = 0
	balance, err = payments.Summarize(pass, 0, 0)
	require.NoError(t, err)
	require.True(t, balance.FullyPaid)
	require.True(t, balance.DepositSatisfied)
}
func TestPaymentDeadlinesUseAssignedTerms(t *testing.T) {
	t.Parallel()
	initial := time.Date(2026, time.October, 14, 15, 0, 0, 0, time.UTC)
	final := initial.Add(24 * time.Hour)
	pass := registrations.Pass{PaymentDueAt: initial, Deposit: &registrations.DepositSnapshot{FinalDueAt: final}}
	due, err := payments.Deadline(pass, payments.Initial)
	require.NoError(t, err)
	require.Equal(t, initial, due)
	due, err = payments.Deadline(pass, payments.TopUp)
	require.NoError(t, err)
	require.Equal(t, final, due)
	pass.Deposit = nil
	_, err = payments.Deadline(pass, payments.TopUp)
	require.ErrorIs(t, err, payments.ErrInvalid)
}
