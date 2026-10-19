package events_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/events"
)

func TestEventTimeZonesAndDSTUseAbsoluteInstants(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, zone string
		offset     int
	}{
		{"moscow", "Europe/Moscow", 3 * 60 * 60}, {"minsk", "Europe/Minsk", 3 * 60 * 60}, {"ekaterinburg", "Asia/Yekaterinburg", 5 * 60 * 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			start := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
			doc, err := events.Normalize(
				events.Document{
					Name:     "Event",
					Timezone: test.zone,
					StartsAt: start,
					EndsAt:   start.Add(time.Hour),
					Modules:  []string{"orders", "massage"},
				},
			)
			require.NoError(t, err)
			local, _, err := (events.Event{Document: doc}).LocalTimes()
			require.NoError(t, err)
			_, offset := local.Zone()
			require.Equal(t, test.offset, offset)
			require.True(t, start.Equal(local))
		})
	}
	start := time.Date(2026, time.March, 29, 0, 30, 0, 0, time.UTC)
	localStart, localEnd, err := (events.Event{Document: events.Document{Timezone: "Europe/Berlin", StartsAt: start, EndsAt: start.Add(time.Hour)}}).LocalTimes()
	require.NoError(t, err)
	require.Equal(t, 1, localStart.Hour())
	require.Equal(t, 3, localEnd.Hour())
	require.Equal(t, time.Hour, localEnd.Sub(localStart))
}

func TestInvalidEventConfigurationCannotBeNormalized(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, time.January, 10, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name, zone string
		modules    []string
		end        time.Time
	}{
		{"system-local", "Local", nil, start.Add(time.Hour)},
		{"unknown-zone", "Europe/Unknown", nil, start.Add(time.Hour)},
		{"duplicate-module", "UTC", []string{"orders", "orders"}, start.Add(time.Hour)},
		{"invalid-module", "UTC", []string{"../orders"}, start.Add(time.Hour)},
		{"nonpositive-window", "UTC", nil, start},
		{"submicrosecond-window", "UTC", nil, start.Add(time.Nanosecond)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := events.Normalize(
				events.Document{
					Name:     "Event",
					Timezone: test.zone,
					StartsAt: start,
					EndsAt:   test.end,
					Modules:  test.modules,
				},
			)
			require.ErrorIs(t, err, events.ErrInvalid)
		})
	}
}
