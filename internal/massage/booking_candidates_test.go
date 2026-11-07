package massage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestPublicBookingCandidatesApplyLeadAndKeepRealObservation(t *testing.T) {
	t.Parallel()
	document := schedule()
	start := document.Windows[0].StartsAt
	observed := start.Add(-massage.ClientBookingLead).Add(time.Microsecond)
	query := massage.ScheduleCandidateQuery{
		WindowID:     "overnight",
		SpecialistID: "alex",
		ServiceID:    "short",
		From:         start,
		Until:        start.Add(80 * time.Minute),
		Limit:        2,
	}
	page, err := massage.PublicBookingCandidateStarts(document, query, observed, "Europe/Berlin")
	require.NoError(t, err)
	require.Equal(t, observed.UTC(), page.ObservedAt)
	require.Len(t, page.Candidates, 2)
	require.Equal(t, 20*time.Minute, page.Candidates[0].StartsAt.Sub(start))
	require.True(t, page.HasMore)
	require.True(t, page.ScheduleOnly, "Geometry alone is not live availability")
	query.AfterStart = page.NextAfter
	next, err := massage.PublicBookingCandidateStarts(document, query, observed, "Europe/Berlin")
	require.NoError(t, err)
	require.Len(t, next.Candidates, 1)
	require.Equal(t, time.Hour, next.Candidates[0].StartsAt.Sub(start))
	document.Services[0].Blocks = 6
	_, err = massage.PublicBookingCandidateStarts(document, query, observed, "Europe/Berlin")
	require.ErrorIs(t, err, massage.ErrUnavailable)
	document = schedule()
	overlapping := document.Windows[0]
	overlapping.ID = "overlap"
	document.Windows = append(document.Windows, overlapping)
	_, err = massage.PublicBookingCandidateStarts(document, query, observed, "Europe/Berlin")
	require.ErrorIs(t, err, massage.ErrBookingTopology)
}
