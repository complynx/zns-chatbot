package api

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestMassageApprovalDescriptionKeepsLocalTermsAndQuotesUntrustedLabels(t *testing.T) {
	t.Parallel()
	start, err := time.Parse(time.RFC3339, "2026-10-25T02:00:00+01:00")
	require.NoError(t, err)
	label := massage.ScheduleLabel{EN: strings.Repeat("😀", 128), RU: strings.Repeat("😀", 128)}
	snapshot := db.MassageBookingSnapshot{EventName: strings.Repeat("`", 256), Terms: massage.BookingTerms{
		BookingSelection: massage.BookingSelection{
			StartsAt: start,
		},
		WindowName:      label,
		SpecialistName:  label,
		ServiceName:     label,
		Timezone:        "Europe/Berlin",
		TreatmentEndsAt: start.Add(15 * time.Minute),
		OccupancyEndsAt: start.Add(20 * time.Minute),
		Currency:        "BYN",
		CurrencyScale:   2,
		PriceMinor:      4500,
	}}
	for _, purpose := range []string{"", "staff_self"} {
		snapshot.Purpose = purpose
		for _, language := range []string{"en", "ru"} {
			for _, cancel := range []bool{false, true} {
				description, e := massageBookingDescription(snapshot, language, cancel)
				require.NoError(t, e)
				require.LessOrEqual(t, len(description), 4000)
				require.Contains(t, description, "2026-10-25T02:00:00+01:00")
				require.Contains(t, description, "2026-10-25T02:20:00+01:00 (Europe/Berlin)")
				require.Contains(t, description, "45.00 BYN")
				require.Contains(t, description, "\n"+strings.Repeat("`", 257)+"\n")
				require.True(t, strings.HasSuffix(description, strings.Repeat("`", 257)))
				if purpose == "staff_self" && !cancel {
					if language == "ru" {
						require.Contains(t, description, "Зарезервировать своё время специалиста?")
					} else {
						require.Contains(t, description, "Reserve your own specialist time?")
					}
				}
			}
		}
	}
}
