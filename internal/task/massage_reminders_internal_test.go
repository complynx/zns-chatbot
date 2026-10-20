package task

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func TestMassageReminderTextKeepsAbsoluteDSTTimeAndPlainLabels(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		for _, offset := range []string{"+02:00", "+01:00"} {
			t.Run(language+offset, func(t *testing.T) {
				t.Parallel()
				start, err := time.Parse(time.RFC3339, "2026-10-25T02:30:00"+offset)
				require.NoError(t, err)
				n := db.MassageReminder{
					Booking: db.MassageBooking{
						ID: uuid.New(),
						Snapshot: db.MassageBookingSnapshot{
							EventName: "<b>Fixture event</b>",
							Terms: massage.BookingTerms{
								BookingSelection: massage.BookingSelection{StartsAt: start.UTC()},
								Timezone:         "Europe/Berlin",
								SpecialistName:   massage.ScheduleLabel{EN: "Alex", RU: "Алекс"},
								ServiceName:      massage.ScheduleLabel{EN: "Short", RU: "Короткий"},
								PriceMinor:       4500,
								Currency:         "BYN",
								CurrencyScale:    2,
							},
						},
					},
				}
				text, err := massageReminderText(n, language)
				require.NoError(t, err)
				require.Contains(t, text, "2026-10-25T02:30:00"+offset)
				require.Contains(t, text, "Europe/Berlin")
				require.Contains(t, text, "<b>Fixture event</b>")
				require.Contains(t, text, "45.00 BYN")
				require.NotContains(t, text, "in 60 minutes")
				require.NotContains(t, text, "через час")
				if language == "ru" {
					require.Contains(t, text, "Напоминание о записи на массаж")
					require.Contains(t, text, "Алекс")
				} else {
					require.Contains(t, text, "Massage booking reminder")
					require.Contains(t, text, "Alex")
				}
			})
		}
	}
}

func TestMassageReminderKindsHaveDistinctLocalizedText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ language, advance, final string }{
		{"en", "(advance)", "(final)"},
		{"ru", "(предварительное)", "(заключительное)"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			t.Parallel()
			reminder := db.MassageReminder{
				Booking: db.MassageBooking{
					ID: uuid.New(),
					Snapshot: db.MassageBookingSnapshot{
						EventName: "Fixture event",
						Terms: massage.BookingTerms{
							BookingSelection: massage.BookingSelection{
								StartsAt: time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC),
							},
							Timezone:       "Europe/Berlin",
							SpecialistName: massage.ScheduleLabel{EN: "Alex", RU: "Алекс"},
							ServiceName:    massage.ScheduleLabel{EN: "Massage", RU: "Массаж"},
							PriceMinor:     4500,
							Currency:       "BYN",
							CurrencyScale:  2,
						},
					},
				},
			}
			reminder.Kind, reminder.LeadSeconds = db.MassageReminderLong, 3600
			advance, err := massageReminderText(reminder, tc.language)
			require.NoError(t, err)
			reminder.Kind, reminder.LeadSeconds = db.MassageReminderShort, 600
			final, err := massageReminderText(reminder, tc.language)
			require.NoError(t, err)
			require.NotEqual(t, advance, final)
			require.Contains(t, advance, tc.advance)
			require.Contains(t, final, tc.final)
			for _, text := range []string{advance, final} {
				require.Contains(t, text, "2026-10-25T02:30:00+02:00")
				require.Contains(t, text, "Europe/Berlin")
				require.Contains(t, text, "45.00 BYN")
				require.NotContains(t, text, "in 60 minutes")
				require.NotContains(t, text, "через час")
			}
		})
	}
}
