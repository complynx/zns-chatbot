package bot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestLineupReadRetrievesLateDJ(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "lineup.csv")
	csv := "time,\"Fri, 25.09\"\nroom,Main\n" + strings.Repeat("22:00,EarlyDJ\n", 300) + "23:00,LateDJ\n"
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))
	source, err := agent.LoadLineup(path, 2026, "Europe/Minsk")
	require.NoError(t, err)
	b := &Bot{Lineup: source}
	input := agent.Input{LineupSource: source.Snapshot(time.Now())}
	require.True(t, input.LineupSource.Reads[2].Omitted)
	// A later user turn can use a host-issued continuation token with a fresh
	// request budget; the source still validates it before reading the next page.
	nextTurn := agent.Input{LineupSource: source.Snapshot(time.Now())}
	require.NoError(t, b.performLineupRead(agent.LineupQuery{
		Scope: "full", Cursor: input.LineupSource.Reads[2].NextCursor,
	}, &nextTurn))
	assert.NotEmpty(t, nextTurn.LineupSource.Reads[2].Entries)
	query := agent.LineupQuery{Scope: "full", DJ: "LateDJ"}
	handled, err := b.performContextRead(t.Context(), "owner", 1, agent.Plan{LineupAction: &query}, &input)
	require.NoError(t, err)
	assert.True(t, handled)
	read := input.LineupSource.Reads[2]
	require.Len(t, read.Entries, 1)
	assert.Equal(t, "LateDJ", read.Entries[0].DJ)
	assert.Equal(t, query, read.Query)
	assert.Equal(t, agent.MaxLineupReads-1, input.LineupSource.Remaining)
	input.LineupSource.Remaining = 0
	require.Error(t, b.performLineupRead(query, &input))
}
