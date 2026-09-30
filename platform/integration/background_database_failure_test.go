package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBackgroundDeliverySQLFailureStopsRunAndRecovers(t *testing.T) {
	t.Parallel()
	f := setup(t)
	handle(t, f.b, message(8100, 101, "/start"))
	var pending int
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT count(*) FROM core.delivery_queue WHERE state='pending'`).Scan(&pending))
	require.Positive(t, pending)
	before := chatMessages(t, f, 101)
	_, err := f.db.Exec(t.Context(), `CREATE SEQUENCE core.background_fault_attempt;
CREATE FUNCTION core.reject_test_background() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM nextval('core.background_fault_attempt');
 RAISE EXCEPTION 'private background SQL diagnostic';
END $$;
CREATE TRIGGER reject_test_background BEFORE UPDATE ON core.delivery_queue
FOR EACH ROW WHEN (NEW.state='sending') EXECUTE FUNCTION core.reject_test_background()`)
	require.NoError(t, err)
	runInboxDatabaseFailure(t, f)
	var called bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT is_called FROM core.background_fault_attempt`).Scan(&called))
	require.True(t, called, "the background worker must reach the injected SQL fault")
	require.Equal(t, before, chatMessages(t, f, 101), "failed admission must not call Telegram")
	require.NoError(t, f.db.Ping(t.Context()), "the fault must not depend on the ownership connection failing")
	var remaining int
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT count(*) FROM core.delivery_queue WHERE state='pending'`).Scan(&remaining))
	require.Equal(t, pending, remaining)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER reject_test_background ON core.delivery_queue`)
	require.NoError(t, err)
	ready := func() bool {
		var unfinished int
		queryErr := f.db.QueryRow(t.Context(),
			`SELECT count(*) FROM core.delivery_queue WHERE state NOT IN ('sent','cancelled')`).Scan(&unfinished)
		return queryErr == nil && unfinished == 0
	}
	runInboxUntil(t, f, ready)
	delivered := chatMessages(t, f, 101)
	require.Greater(t, len(delivered), len(before))
	runInboxUntil(t, f, ready)
	require.Equal(t, delivered, chatMessages(t, f, 101), "completed delivery must not repeat on restart")
}
