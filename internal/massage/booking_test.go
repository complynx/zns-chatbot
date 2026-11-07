package massage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestPublicBookingKeepsPriceBufferAndRepeatedLocalTimesDistinct(t *testing.T) {
	t.Parallel()
	document := schedule()
	observed := document.Windows[0].StartsAt.Add(-time.Hour)
	var starts []time.Time
	for _, input := range []string{"2026-10-25T02:00:00+02:00", "2026-10-25T02:00:00+01:00"} {
		selected := massage.BookingSelection{
			WindowID:     "overnight",
			SpecialistID: "alex",
			ServiceID:    "short",
			StartsAt:     instant(input),
		}
		terms, err := massage.PublicBookingTerms(document, selected, observed, "Europe/Berlin")
		require.NoError(t, err)
		require.Equal(t, input, terms.StartsAt.Format(time.RFC3339))
		require.Equal(t, 15*time.Minute, terms.TreatmentEndsAt.Sub(terms.StartsAt))
		require.Equal(t, 20*time.Minute, terms.OccupancyEndsAt.Sub(terms.StartsAt))
		require.Equal(t, int64(4500), terms.PriceMinor)
		require.Equal(t, "BYN", terms.Currency)
		require.Equal(t, "Europe/Berlin", terms.Timezone)
		starts = append(starts, terms.StartsAt)
	}
	require.Equal(t, time.Hour, starts[1].Sub(starts[0]))
}

func TestPublicBookingRequiresLeadGridPublicDurationAndUnambiguousParty(t *testing.T) {
	t.Parallel()
	document := schedule()
	selection := massage.BookingSelection{
		WindowID:     "overnight",
		SpecialistID: "alex",
		ServiceID:    "short",
		StartsAt:     document.Windows[0].StartsAt,
	}
	observed := selection.StartsAt.Add(-massage.ClientBookingLead)
	_, err := massage.PublicBookingTerms(document, selection, observed, "UTC")
	require.NoError(t, err)
	_, err = massage.PublicBookingTerms(document, selection, observed.Add(time.Microsecond), "UTC")
	require.ErrorIs(t, err, massage.ErrUnavailable)
	shifted := selection
	shifted.StartsAt = shifted.StartsAt.Add(time.Minute)
	_, err = massage.PublicBookingTerms(document, shifted, observed, "UTC")
	require.ErrorIs(t, err, massage.ErrUnavailable)
	_, err = massage.PublicBookingTerms(document, selection, time.Time{}, "UTC")
	require.ErrorIs(t, err, massage.ErrInvalid)
	document.Services[0].Blocks = 6
	_, err = massage.PublicBookingTerms(document, selection, observed, "UTC")
	require.ErrorIs(t, err, massage.ErrUnavailable)
	document = schedule()
	other := document.Windows[0]
	other.ID = "overlap"
	other.StartsAt = other.StartsAt.Add(time.Hour)
	document.Windows = append(document.Windows, other)
	_, err = massage.PublicBookingTerms(document, selection, observed, "UTC")
	require.ErrorIs(t, err, massage.ErrBookingTopology)
}

func TestBookingCapacityCountsConcurrentUseAndProtectsOwnerAndSpecialist(t *testing.T) {
	t.Parallel()
	start := instant("2026-10-25T00:00:00Z")
	selection := massage.BookingSelection{StartsAt: start}
	terms := massage.BookingTerms{
		BookingSelection: selection,
		OccupancyEndsAt:  start.Add(20 * time.Minute),
		RequiresTable:    true,
		ConfiguredTables: 2,
	}
	occupied := []massage.BookingOccupation{
		{StartsAt: start.Add(-10 * time.Minute), EndsAt: start.Add(10 * time.Minute), RequiresTable: true},
		{StartsAt: start.Add(10 * time.Minute), EndsAt: start.Add(30 * time.Minute), RequiresTable: true},
	}
	require.True(t, massage.BookingSlotAvailable(terms, occupied), "Consecutive users occupy only one of two tables")
	terms.ConfiguredTables = 1
	require.False(t, massage.BookingSlotAvailable(terms, occupied))
	terms.RequiresTable = false
	require.True(t, massage.BookingSlotAvailable(terms, occupied))
	occupied[0].SameOwner = true
	require.False(t, massage.BookingSlotAvailable(terms, occupied))
	occupied[0].SameOwner = false
	occupied[1].SameSpecialist = true
	require.False(t, massage.BookingSlotAvailable(terms, occupied))
	occupied[1].StartsAt = terms.OccupancyEndsAt
	require.True(
		t,
		massage.BookingSlotAvailable(terms, occupied),
		"A specialist can start exactly at the previous occupied end",
	)
}

func TestBookingTermsSurvivePriceEditsButRejectChangedCommitments(t *testing.T) {
	t.Parallel()
	document := schedule()
	selection := massage.BookingSelection{
		WindowID:     "overnight",
		SpecialistID: "alex",
		ServiceID:    "short",
		StartsAt:     document.Windows[0].StartsAt,
	}
	terms, err := massage.PublicBookingTerms(document, selection, selection.StartsAt.Add(-time.Hour), "Europe/Berlin")
	require.NoError(t, err)
	document.Services[0].PriceMinor++
	document.Services[0].Name.EN = "New label"
	require.True(t, massage.BookingFitsSchedule(document, terms))
	require.Equal(t, int64(4500), terms.PriceMinor)
	require.Equal(t, "Short", terms.ServiceName.EN)
	document.Services[0].Blocks = 2
	require.False(t, massage.BookingFitsSchedule(document, terms))
	document = schedule()
	document.Specialists[0].RequiresTable = false
	require.False(t, massage.BookingFitsSchedule(document, terms))
}
