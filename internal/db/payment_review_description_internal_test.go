package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPaymentReviewMoneyPreservesExactMinorUnitsAndScale(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		amount int64
		scale  int32
		want   string
	}{
		{9007199254740991, 0, "9007199254740991 BYN"},
		{9007199254740991, 1, "900719925474099.1 BYN"},
		{9007199254740991, 2, "90071992547409.91 BYN"},
		{9007199254740991, 3, "9007199254740.991 BYN"},
		{9007199254740991, 4, "900719925474.0991 BYN"},
		{1, 4, "0.0001 BYN"}, {0, 2, "0.00 BYN"},
	} {
		require.Equal(t, test.want, paymentReviewMoney(test.amount, test.scale, "BYN"))
	}
}
