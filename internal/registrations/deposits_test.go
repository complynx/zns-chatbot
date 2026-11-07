package registrations_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestDepositMinimumHonorsAmountAndRoundsShareUpExactly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		price, amount int64
		share         int32
		want          int64
	}{
		{"share dominates", 12345, 3000, 2500, 3087},
		{"amount dominates", 12345, 4000, 2500, 4000},
		{"full price", 12345, 0, 10000, 12345},
		{"zero price", 0, 0, 5000, 0},
		{"large exact price", 9007199254740991, 0, 9999, 9006298534815517},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := (registrations.DepositPolicy{MinimumMinor: tc.amount, MinimumBPS: tc.share}).Threshold(
				tc.price,
			)
			require.NoError(t, err)
			require.Equal(t, tc.want, result)
		})
	}
}
