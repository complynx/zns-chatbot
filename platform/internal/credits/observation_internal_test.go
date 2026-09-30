package credits

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageObservationKnownZeroUnknownAndPrecision(t *testing.T) {
	t.Parallel()
	var item UsageObservation
	zero := int64(0)
	maximum := int64(math.MaxInt64)
	require.NoError(t, item.add("reserved", Usage{}))
	require.NoError(t, item.add("dispatched", Usage{}))
	require.NoError(t, item.add("not_sent", Usage{}))
	require.NoError(t, item.add("settled", Usage{Basis: "unknown"}))
	require.NoError(t, item.add("settled", Usage{Basis: "reported", Input: &maximum, Output: &zero}))
	require.Equal(t, [4]int64{1, 1, 2, 1}, item.States)
	require.Equal(t, [4]int64{1, 0, 1, 0}, item.Bases)
	require.Equal(
		t,
		UsageTokenObservation{Sum: UsageObservationExactMaximum, KnownReceipts: 1, UnknownReceipts: 1, Capped: true},
		item.Tokens[0],
	)
	require.Equal(
		t,
		UsageTokenObservation{KnownReceipts: 1, UnknownReceipts: 1},
		item.Tokens[3],
		"known zero differs from missing",
	)
	require.Equal(t, int64(2), item.Tokens[1].UnknownReceipts)
	exact := UsageObservationExactMaximum
	var boundary UsageObservation
	require.NoError(t, boundary.add("settled", Usage{Basis: "reported", Input: &exact}))
	require.False(t, boundary.Tokens[0].Capped, "largest exact value remains exact")
	one := int64(1)
	require.NoError(t, boundary.add("settled", Usage{Basis: "estimated", Input: &one}))
	require.True(t, boundary.Tokens[0].Capped)
	require.Equal(t, UsageObservationExactMaximum, boundary.Tokens[0].Sum)
}

func TestUsageObservationRejectsInvalidPersistedCounters(t *testing.T) {
	t.Parallel()
	negative := int64(-1)
	for _, usage := range []Usage{{Basis: "arbitrary-private"}, {Basis: "reported", Input: &negative}} {
		var item UsageObservation
		require.ErrorIs(t, item.add("settled", usage), ErrInvalid)
		require.Equal(t, UsageObservation{}, item)
	}
	var item UsageObservation
	require.ErrorIs(t, item.add("arbitrary-private", Usage{}), ErrInvalid)
	_, err := (Service{}).ObserveUsage(t.Context())
	require.Error(t, err)
}
