package lineup_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/lineup"
)

func TestQueriesPreserveSourceSemantics(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Europe/Minsk")
	require.NoError(t, err)
	schedule, err := lineup.Parse(strings.NewReader(`time,"Fri, 25.09","Fri, 25.09","Sat, 26.09"
room,Main,Second,Main
22:00,Alpha,Beta,Gamma
00:00,Night,,Next
06:00,Dawn,,
invalid,Ignored,,
07:00,Early,,
23:00
`), 2026, location)
	require.NoError(t, err)
	now := time.Date(2026, time.September, 25, 22, 0, 0, 0, location)
	assert.Len(t, schedule.Current(now.UTC()), 2)
	assert.Empty(t, schedule.Current(now.Add(time.Hour)))
	assert.Len(t, schedule.Day(now), 5)
	assert.Len(t, schedule.Day(now.Add(4*time.Hour)), 5)
	// Source includes the previous night's rows on the target calendar date.
	assert.Len(t, schedule.Day(now.Add(12*time.Hour)), 4)
	full := schedule.Full()
	require.Len(t, full, 3)
	assert.Equal(t, "Main", full[0].Room)
	assert.Equal(t, "Early", full[0].Entries[0].DJ)
	assert.Equal(t, "Dawn", full[0].Entries[3].DJ)
	assert.Equal(t, "2026-09-25", full[0].Date.Format(time.DateOnly))
	assert.Empty(t, schedule.Day(now.AddDate(0, 0, 4)))
	full[0].Entries[0].DJ = "modified"
	assert.Equal(t, "Early", schedule.Full()[0].Entries[0].DJ)
}

func TestYearRollover(t *testing.T) {
	t.Parallel()
	schedule, err := lineup.Parse(strings.NewReader("time,\"Wed, 31.12\"\nroom,A\n00:00,New year\n"), 2025, time.UTC)
	require.NoError(t, err)
	entries := schedule.Current(time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC))
	require.Len(t, entries, 1)
	assert.Equal(t, "2025-12-31", schedule.Full()[0].Date.Format(time.DateOnly))
}

func TestDST(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Europe/Amsterdam")
	require.NoError(t, err)
	for _, test := range []struct {
		name, date, clock, problem string
	}{
		{"spring gap", "28.03", "02:30", "nonexistent local time"},
		{"autumn fold", "24.10", "02:30", "ambiguous local time"},
		{"after spring", "28.03", "03:00", ""},
		{"after autumn", "24.10", "03:00", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := "time,\"Day, " + test.date + "\"\nroom,A\n" + test.clock + ",DJ\n"
			schedule, parseErr := lineup.Parse(strings.NewReader(source), 2026, location)
			if test.problem != "" {
				require.ErrorContains(t, parseErr, test.problem)
				return
			}
			require.NoError(t, parseErr)
			entry := schedule.Full()[0].Entries[0]
			assert.Len(t, schedule.Current(entry.Start), 1)
			assert.Empty(t, schedule.Current(entry.Start.Add(time.Hour)))
			assert.Len(t, schedule.Day(entry.Start), 1)
		})
	}
}

func TestInvalidCSV(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"time,date\nroom,A\n",
		"time,\"Day, 31.02\"\nroom,A\n",
		"time,\"Day, 25.09\"\n",
		"time,\"Day, 25.09\"\nroom\n",
		"time,\"Day, 25.09\"\nroom, \n",
		"\"unfinished",
		string([]byte{0xff}),
		strings.Repeat("x", lineup.MaxBytes+1),
		"time,\"Day, 25.09\"\nroom,A\n" + strings.Repeat("22:00,DJ\n", 10000),
		strings.Repeat("date,", 128) + "date\n" + strings.Repeat("room,", 128) + "room\n",
	} {
		_, err := lineup.Parse(strings.NewReader(source), 2026, time.UTC)
		require.Error(t, err)
	}
	_, err := lineup.Parse(strings.NewReader(""), 0, time.UTC)
	require.Error(t, err)
	_, err = lineup.Parse(strings.NewReader(""), 2026, nil)
	require.Error(t, err)
	schedule, err := lineup.Parse(strings.NewReader(""), 2026, time.UTC)
	require.NoError(t, err)
	assert.Empty(t, schedule.Current(time.Now()))
	assert.Empty(t, schedule.Day(time.Now()))
	assert.Empty(t, schedule.Full())
}
