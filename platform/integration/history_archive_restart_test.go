package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

// A real inbox run loses its archive race after final validation. A failed
// acknowledgement then forces a fresh host to consume the durable terminal plan.
func TestHistoryArchiveRejectionSurvivesInboxRestart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive := conversation.Service{DB: f.db}
	const canary = "deleted archive-boundary lighthouse canary"
	require.NoError(t, archive.AppendOriginal(t.Context(), "alice", "archive-race-source", "user", canary))
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='archive-race-source'`).
			Scan(&id),
	)
	var calls atomic.Int64
	f.b.Model = avModel(func(_ context.Context, in agent.Input) (agent.Plan, error) {
		calls.Add(1)
		raw, err := json.Marshal(in)
		if err != nil {
			return agent.Plan{}, err
		}
		assert.Contains(t, string(raw), canary)
		return agent.Plan{View: "workflow", Text: canary}, nil
	})
	var deleted atomic.Bool
	f.b.API.HTTP = &http.Client{Transport: qaHistoryDeleteTransport{before: func(r *http.Request) error {
		if r.URL.Path != "/internal/history/archive/derived" {
			return nil
		}
		body, err := r.GetBody()
		if err != nil {
			return err
		}
		defer body.Close()
		var payload struct {
			Kind               string `json:"kind"`
			ExpectedGeneration *int64 `json:"expected_generation"`
		}
		if err = json.NewDecoder(body).Decode(&payload); err != nil {
			return err
		}
		if deleted.CompareAndSwap(false, true) {
			assert.NotNil(t, payload.ExpectedGeneration)
			return archive.DeleteContent(r.Context(), "alice", id)
		}
		return nil
	}}}
	f.b.Host.HTTP = f.b.API.HTTP
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "recall the old detail"})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "following request"})
	_, err := f.db.Exec(t.Context(), `CREATE FUNCTION bot.hold_archive_ack() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF OLD.update_id=1 THEN RAISE EXCEPTION 'synthetic acknowledgement failure'; END IF; RETURN OLD; END $$;
 CREATE TRIGGER hold_archive_ack BEFORE DELETE ON bot.telegram_inbox FOR EACH ROW EXECUTE FUNCTION bot.hold_archive_ack()`)
	require.NoError(t, err)
	runInboxDatabaseFailure(t, f)
	var terminal bool
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 kind='terminal' AND state='privacy_terminal' AND reason='history_deleted'
 FROM interaction.saved_turns WHERE owner='alice' AND update_id=1`).Scan(&terminal))
	require.True(t, terminal)
	var failures int
	require.NoError(t,
		f.db.QueryRow(t.Context(), `SELECT failures FROM bot.telegram_inbox WHERE update_id=1`).Scan(&failures),
	)
	require.Zero(t, failures, "SQL acknowledgement failure must not consume the poison budget")
	require.True(t, deleted.Load())
	require.EqualValues(t, 1, calls.Load())
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE update_id=1 AND kind='reply'`).
			Scan(&count),
	)
	require.Zero(t, count, "stale reply must not be renderable after rejected archive")
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE text LIKE '%'||$1||'%'`, canary).
			Scan(&count),
	)
	require.Zero(t, count)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.telegram_inbox`).Scan(&count))
	require.Equal(t, 2, count)
	_, err = f.db.Exec(t.Context(), `DROP TRIGGER hold_archive_ack ON bot.telegram_inbox`)
	require.NoError(t, err)
	restarted := *f.b
	f.b = &restarted
	f.b.Model = avModel(func(_ context.Context, in agent.Input) (agent.Plan, error) {
		calls.Add(1)
		assert.Equal(t, "following request", in.Text)
		raw, encodeErr := json.Marshal(in)
		if encodeErr != nil {
			return agent.Plan{}, encodeErr
		}
		assert.NotContains(t, string(raw), canary)
		return agent.Plan{View: "workflow", Text: "fresh reply"}, nil
	})
	completeInboxAfterCooldown(t, f, 3, 1)
	require.EqualValues(t, 2, calls.Load())
	restartedAgain := *f.b
	f.b = &restartedAgain
	completeInbox(t, f, 3)
	require.EqualValues(t, 2, calls.Load())
}
