package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lineupFixture(t *testing.T, csv string) *LineupSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lineup.csv")
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))
	source, err := LoadLineup(path, 2026, "Europe/Minsk")
	require.NoError(t, err)
	return source
}

func TestLineupSelectedScopeOnly(t *testing.T) {
	t.Parallel()
	source := lineupFixture(t, "time,\"Fri, 25.09\"\nroom,Main\n22:00,CurrentDJ\n23:00,LaterDJ\n")
	now := time.Date(2026, time.September, 25, 19, 30, 0, 0, time.UTC)
	for _, selected := range []string{"", "lineup_current", "lineup_day", "lineup_full"} {
		t.Run(selected, func(t *testing.T) {
			t.Parallel()
			input := Input{Text: "question", LineupSource: source.Snapshot(now)}
			// The private model transport retains source data, unlike provider input.
			encoded, err := remoteInput(input)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(encoded, &input))
			require.NotNil(t, input.LineupSource)
			calls := 0
			_, err = planWithSkills(t.Context(), input, func(_ context.Context, prompt providerPrompt) (string, error) {
				calls++
				assert.Nil(t, prompt.source.LineupSource)
				assert.NotContains(t, string(prompt.input), "lineup_source")
				if calls == 1 {
					assert.NotContains(t, string(prompt.input), "CurrentDJ")
					assert.NotContains(t, string(prompt.input), `"lineup"`)
					if selected == "" {
						return `{"skills":[],"reply_language":"en"}`, nil
					}
					return `{"skills":["` + selected + `"],"reply_language":"ru"}`, nil
				}
				if selected == "" {
					assert.Nil(t, prompt.source.Lineup)
					assert.NotContains(t, string(prompt.input), "CurrentDJ")
				} else {
					require.NotNil(t, prompt.source.Lineup)
					require.Len(t, prompt.source.Lineup.Reads, 1)
					assert.Equal(t, strings.TrimPrefix(selected, "lineup_"), prompt.source.Lineup.Reads[0].Scope)
					assert.Contains(t, string(prompt.input), "CurrentDJ")
					assert.Contains(t, prompt.instructions, "unavailable")
					if selected == "lineup_current" {
						assert.NotContains(t, string(prompt.input), "LaterDJ")
					}
				}
				return emptyActionsPlan, nil
			})
			require.NoError(t, err)
			assert.Equal(t, 2, calls)
		})
	}
}

func TestLineupAvailabilityAndClock(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 25, 21, 30, 0, 0, time.UTC)
	source := lineupFixture(t, "time,\"Fri, 25.09\"\nroom,Main\n00:00,NightDJ\n")
	result := source.Snapshot(now)
	assert.Equal(t, "2026-09-26T00:30:00+03:00", result.Now.Format(time.RFC3339))
	assert.Equal(t, "Europe/Minsk", result.Timezone)
	assert.Equal(t, "available", result.Reads[0].Status)
	assert.Equal(t, "2026-09-25", result.Reads[1].Entries[0].EventDate)
	later := source.Snapshot(now.Add(time.Hour))
	assert.Equal(t, "no_current_party", later.Reads[0].Status)
	assert.Equal(t, "available", later.Reads[1].Status)
	next := source.Snapshot(now.AddDate(0, 0, 2))
	assert.Equal(t, "no_party_today", next.Reads[1].Status)
	empty := lineupFixture(t, "").Snapshot(now)
	assert.Equal(t, "no_current_party", empty.Reads[0].Status)
	assert.Equal(t, "no_party_today", empty.Reads[1].Status)
	assert.Equal(t, "no_schedule", empty.Reads[2].Status)
	var unavailable *LineupSource
	for _, read := range unavailable.Snapshot(now).Reads {
		assert.Equal(t, "unavailable", read.Status)
	}
}

func TestLineupLoadFailuresAndOmission(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "private-missing-path.csv")
	source, err := LoadLineup(path, 2026, "Europe/Minsk")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), path)
	assert.Nil(t, source)
	require.NoError(t, os.WriteFile(path, []byte("bad CSV secret"), 0o600))
	_, err = LoadLineup(path, 2026, "Europe/Minsk")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	_, err = LoadLineup(path, 2026, "Local")
	require.Error(t, err)
	source = lineupFixture(t, "time,\"Fri, 25.09\"\nroom,Main\n22:00,"+strings.Repeat("x", maxLineupReadBytes)+"\n")
	result := source.Snapshot(time.Date(2026, time.September, 25, 19, 30, 0, 0, time.UTC))
	for _, read := range result.Reads {
		assert.Equal(t, "available", read.Status)
		assert.False(t, read.Omitted)
		require.Len(t, read.Entries, 1)
		assert.True(t, read.Entries[0].Truncated)
	}
}
