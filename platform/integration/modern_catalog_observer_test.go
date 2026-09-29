package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// Native runs retain the five-second responsiveness assertion. Race runs use
// a separate bounded correctness observation: measured instrumented completion
// was 9.13s versus native completion below five seconds. Product waits and all
// existing tests are unchanged; the allowance is only for this large fixture.
func catalogRuntimeScript(t *testing.T, f *fixture, code string) json.RawMessage {
	t.Helper()
	update := queueModernRuntimePlan(t, f, 101, "en", "Read the catalog",
		agent.Plan{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		agent.Plan{View: agent.OrdersView, Text: "Catalog read recorded."})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.b.Run(ctx) }()
	defer func() {
		cancel()
		require.NoError(t, <-done)
	}()
	started := time.Now()
	require.Eventually(t, func() bool {
		var cursor int64
		var count int
		err := f.db.QueryRow(t.Context(), `SELECT value,(SELECT count(*) FROM bot.telegram_inbox)
FROM bot.cursors WHERE name='telegram'`).Scan(&cursor, &count)
		return err == nil && cursor == update+1 && count == 0
	}, catalogRuntimeObservationSeconds*time.Second, 10*time.Millisecond)
	t.Logf(
		"Catalog inbox complete in %s; observation allowance %ds",
		time.Since(started),
		catalogRuntimeObservationSeconds,
	)
	assertModernRuntimeFixtureConsumed(t, f, update, 2)
	var records []struct {
		Run agent.ScriptRun `json:"run"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&records),
	)
	require.Len(t, records, 1)
	require.Empty(t, records[0].Run.Error)
	return records[0].Run.Result
}
