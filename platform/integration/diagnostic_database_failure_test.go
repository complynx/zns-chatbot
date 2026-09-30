package integration_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestDiagnosticSQLFailureStopsBeforeModelAndRecoversInbox(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Logger = observability.NewLogger(io.Discard, observability.LogConfig{})
	_, err := f.db.Exec(
		t.Context(),
		`CREATE FUNCTION bot.reject_test_diagnostics() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'private diagnostics SQL failure'; END $$;
CREATE TRIGGER reject_test_diagnostics BEFORE INSERT OR UPDATE ON bot.interactions
FOR EACH ROW WHEN (NEW.kind='diagnostic_context') EXECUTE FUNCTION bot.reject_test_diagnostics()`,
	)
	require.NoError(t, err)
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "hello"})
	runInboxDatabaseFailure(t, f)
	require.Zero(t, f.model.calls, "a failed SQL operation must stop before planning")
	var pending, diagnostics int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
(SELECT count(*) FROM bot.telegram_inbox),
(SELECT count(*) FROM bot.interactions WHERE kind='diagnostic_context')`).Scan(&pending, &diagnostics))
	require.Equal(t, 1, pending)
	require.Zero(t, diagnostics)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_test_diagnostics ON bot.interactions`)
	require.NoError(t, err)
	completeInbox(t, f, 2)
	require.Equal(t, 1, f.model.calls, "restart must process the retained input once")
}
