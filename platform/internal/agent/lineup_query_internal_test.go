package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLineupPaginationReachesEveryEntry(t *testing.T) {
	t.Parallel()
	var csv strings.Builder
	csv.WriteString("time,\"Fri, 25.09\"\nroom,Main\n")
	const count = 300
	for index := range count {
		fmt.Fprintf(&csv, "22:00,DJ-%03d\n", index)
	}
	source := lineupFixture(t, csv.String())
	now := time.Date(2026, time.September, 25, 19, 0, 0, 0, time.UTC)
	for _, scope := range []string{"full", "day", "current"} {
		query := LineupQuery{Scope: scope}
		var names []string
		for {
			page, err := source.Query(now, query)
			require.NoError(t, err)
			encoded, err := json.Marshal(page)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(encoded), maxLineupReadBytes)
			for _, entry := range page.Entries {
				names = append(names, entry.DJ)
			}
			if !page.Omitted {
				break
			}
			require.NotEmpty(t, page.NextCursor)
			query.Cursor = page.NextCursor
		}
		require.Len(t, names, count)
		assert.Equal(t, "DJ-299", names[count-1])
		filtered, err := source.Query(now, LineupQuery{Scope: scope, DJ: "dj-299", Room: "MAIN", Date: "2026-09-25"})
		require.NoError(t, err)
		require.Len(t, filtered.Entries, 1)
		assert.Equal(t, "DJ-299", filtered.Entries[0].DJ)
		assert.False(t, filtered.Omitted)
	}
}

func TestLineupQueryValidation(t *testing.T) {
	t.Parallel()
	for _, query := range []LineupQuery{
		{Scope: "unknown"}, {Scope: "full", Cursor: strings.Repeat("x", maxLineupCursorBytes+1)}, {Scope: "day", Date: "2026-02-30"},
		{Scope: "full", Room: strings.Repeat("x", 129)},
	} {
		require.Error(t, query.Validate())
	}
	query := &LineupQuery{Scope: "full", DJ: "LateDJ"}
	require.NoError(t, Validate(Plan{View: workflowView, LineupAction: query}))
	require.Error(t, Validate(Plan{View: workflowView, LineupAction: query, HistoryAction: &HistoryProposal{}}))
	plan, err := decodePlan(
		`{"text":"","view":"workflow","lineup_action":{"scope":"full","date":"","room":"","dj":"LateDJ","cursor":""}}`,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, query, plan.LineupAction)
	source := lineupFixture(t, "time,\"Fri, 25.09\"\nroom,Main\n22:00,OtherDJ\n")
	read, err := source.Query(time.Now(), *query)
	require.NoError(t, err)
	assert.Equal(t, "no_matches", read.Status)
}

func TestLineupCursorBindsSourceFiltersAndInstant(t *testing.T) {
	t.Parallel()
	csv := "time,\"Fri, 25.09\"\nroom,Main\n" + strings.Repeat("22:00,DJ\n", 300)
	source := lineupFixture(t, csv)
	now := time.Date(2026, time.September, 25, 19, 0, 0, 0, time.UTC)
	page, err := source.Query(now, LineupQuery{Scope: "current"})
	require.NoError(t, err)
	require.True(t, page.Omitted)
	require.NotEmpty(t, page.NextCursor)
	query := LineupQuery{Scope: "current", Cursor: page.NextCursor}
	continued, err := source.Query(now.AddDate(0, 0, 1), query)
	require.NoError(t, err)
	assert.True(t, continued.AsOf.Equal(now))
	assert.NotEmpty(t, continued.Entries)
	changed := query
	changed.DJ = "DJ"
	_, err = source.Query(now, changed)
	require.Error(t, err)
	changed = query
	changed.Scope = "full"
	_, err = source.Query(now, changed)
	require.Error(t, err)
	changed = query
	changed.Cursor = "forged" + changed.Cursor
	_, err = source.Query(now, changed)
	require.Error(t, err)
	_, err = lineupFixture(t, csv).Query(now, query)
	require.Error(t, err)
}
