package bot

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreditAmountInputIsExact(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", ".", ".5", "-1", "+1", "1e3", "1.1234567890", "9223372037"} {
		_, valid := creditPolicyChange(raw, 1, 2)
		require.False(t, valid, raw)
	}
	for raw, want := range map[string]int64{"0": 0, "1": 1_000_000_000, "0.000000001": 1, "9223372036.854775807": 9223372036854775807} {
		value, valid := creditPolicyChange(raw, 1, 2)
		require.True(t, valid, raw)
		require.Equal(t, want, *value.MonthlyNanoUSD)
		require.Equal(t, raw, creditAmount(want))
	}
}

func TestCreditSignedAdjustmentsAreExact(t *testing.T) {
	t.Parallel()
	require.Equal(t, "-0.05", creditAmount(-50_000_000))
	require.Equal(t, "-9223372036.854775808", creditAmount(-9223372036854775808))
}
