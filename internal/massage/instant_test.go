package massage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestInstantBookingUsesCurrentGridAndFiveMinuteGrace(t *testing.T) {
	t.Parallel()
	document := schedule()
	anchor := document.Windows[0].StartsAt
	selection := massage.InstantSelection{WindowID: "overnight", SpecialistID: "alex", ServiceID: "short"}
	for _, test := range []struct {
		name     string
		observed time.Duration
		start    time.Duration
		denied   bool
	}{
		{name: "window begins", observed: 0, start: 0},
		{name: "exact grace", observed: 5 * time.Minute, start: 0},
		{name: "past grace", observed: 5*time.Minute + time.Microsecond, start: 20 * time.Minute},
		{name: "next grid", observed: 23 * time.Minute, start: 20 * time.Minute},
		{name: "negative floor at grace", observed: -15 * time.Minute, denied: true},
		{name: "advance from negative floor", observed: -15*time.Minute + time.Microsecond, start: 0},
		{name: "too early", observed: -2 * time.Hour, denied: true},
		{name: "window ended", observed: 4 * time.Hour, denied: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			terms, err := massage.CurrentInstantTerms(document, selection, anchor.Add(test.observed), "Europe/Berlin")
			if test.denied {
				require.ErrorIs(t, err, massage.ErrUnavailable)
				return
			}
			require.NoError(t, err)
			require.True(t, terms.StartsAt.Equal(anchor.Add(test.start)))
			require.Equal(t, 15*time.Minute, terms.TreatmentEndsAt.Sub(terms.StartsAt))
			require.Equal(t, 20*time.Minute, terms.OccupancyEndsAt.Sub(terms.StartsAt))
			require.EqualValues(t, 4500, terms.PriceMinor)
		})
	}
}

func TestInstantBookingFrozenConsentExpiresInsteadOfMoving(t *testing.T) {
	t.Parallel()
	document := schedule()
	anchor := document.Windows[0].StartsAt
	terms, err := massage.CurrentInstantTerms(document, massage.InstantSelection{
		WindowID: "overnight", SpecialistID: "alex", ServiceID: "short",
	}, anchor, "Europe/Berlin")
	require.NoError(t, err)
	frozen, err := massage.FrozenInstantTerms(document, terms.BookingSelection,
		anchor.Add(5*time.Minute), "Europe/Berlin")
	require.NoError(t, err)
	require.Equal(t, terms, frozen)
	_, err = massage.FrozenInstantTerms(document, terms.BookingSelection,
		anchor.Add(5*time.Minute+time.Microsecond), "Europe/Berlin")
	require.ErrorIs(t, err, massage.ErrUnavailable)
}

func TestInstantBookingSupportsConfiguredSixBlocksWithoutChangingPublicRules(t *testing.T) {
	t.Parallel()
	document := schedule()
	document.Services[0].Blocks = 6
	anchor := document.Windows[0].StartsAt
	terms, err := massage.CurrentInstantTerms(document, massage.InstantSelection{
		WindowID: "overnight", SpecialistID: "alex", ServiceID: "short",
	}, anchor, "Europe/Berlin")
	require.NoError(t, err)
	require.Equal(t, 2*time.Hour, terms.OccupancyEndsAt.Sub(terms.StartsAt))
	require.EqualValues(t, 4500, terms.PriceMinor, "Use the configured price, never manufacture a length-based price")
	_, err = massage.PublicBookingTerms(document, terms.BookingSelection,
		anchor.Add(-time.Hour), "Europe/Berlin")
	require.ErrorIs(t, err, massage.ErrUnavailable)
	document.Specialists[0].ServiceIDs = []string{"long"}
	_, err = massage.CurrentInstantTerms(document, massage.InstantSelection{
		WindowID: "overnight", SpecialistID: "alex", ServiceID: "short",
	}, anchor, "Europe/Berlin")
	require.Error(t, err)
}

func TestInstantBookingShortGridDoesNotChooseAnOlderEligibleSlot(t *testing.T) {
	t.Parallel()
	document := schedule()
	document.BlockSeconds, document.BufferSeconds = 60, 0
	anchor := document.Windows[0].StartsAt
	terms, err := massage.CurrentInstantTerms(document, massage.InstantSelection{
		WindowID: "overnight", SpecialistID: "alex", ServiceID: "short",
	}, anchor.Add(4*time.Minute+30*time.Second), "Europe/Berlin")
	require.NoError(t, err)
	require.True(t, terms.StartsAt.Equal(anchor.Add(4*time.Minute)))
}

func TestInstantBookingDoesNotSkipAWorkSpanGap(t *testing.T) {
	t.Parallel()
	document := schedule()
	anchor := document.Windows[0].StartsAt
	document.Specialists[0].WorkSpans[0].StartsAt = anchor.Add(40 * time.Minute)
	_, err := massage.CurrentInstantTerms(document, massage.InstantSelection{
		WindowID: "overnight", SpecialistID: "alex", ServiceID: "short",
	}, anchor, "Europe/Berlin")
	require.ErrorIs(t, err, massage.ErrUnavailable)
}

func TestInstantBookingDistinguishesRepeatedLocalHour(t *testing.T) {
	t.Parallel()
	document := schedule()
	for _, start := range []string{"2026-10-25T02:00:00+02:00", "2026-10-25T02:00:00+01:00"} {
		terms, err := massage.CurrentInstantTerms(document, massage.InstantSelection{
			WindowID: "overnight", SpecialistID: "alex", ServiceID: "short",
		}, instant(start).Add(3*time.Minute), "Europe/Berlin")
		require.NoError(t, err)
		require.Equal(t, start, terms.StartsAt.Format(time.RFC3339))
	}
}
