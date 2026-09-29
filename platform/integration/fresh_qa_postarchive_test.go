package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

type freshAfterTransport struct {
	after func(*http.Request, *http.Response) error
}

func (tr freshAfterTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return response, err
	}
	if err = tr.after(r, response); err != nil {
		response.Body.Close()
		return nil, err
	}
	return response, nil
}

func TestFreshQADeletionAfterArchiveBeforeRendering(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive := conversation.Service{DB: f.db}
	const canary = "violet garden postarchive deletion canary"
	require.NoError(t, archive.AppendOriginal(t.Context(), "alice", "fresh-source", "user", canary))
	var source int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='fresh-source'`).
			Scan(&source),
	)
	f.b.Model = avModel(func(_ context.Context, in agent.Input) (agent.Plan, error) {
		raw, err := json.Marshal(in)
		require.NoError(t, err)
		require.Contains(t, string(raw), canary)
		return agent.Plan{View: "workflow", Text: canary}, nil
	})
	deleted := false
	f.b.API.HTTP = &http.Client{
		Transport: freshAfterTransport{after: func(r *http.Request, response *http.Response) error {
			if r.URL.Path != "/internal/history/archive/derived" || response.StatusCode != http.StatusOK {
				return nil
			}
			body, err := r.GetBody()
			if err != nil {
				return err
			}
			defer body.Close()
			var p struct {
				Kind string `json:"kind"`
			}
			if err = json.NewDecoder(body).Decode(&p); err != nil {
				return err
			}
			if !deleted {
				deleted = true
				return archive.DeleteContent(r.Context(), "alice", source)
			}
			return nil
		}},
	}
	f.b.Host.HTTP = f.b.API.HTTP
	err := f.b.Handle(t.Context(), message(99101, 101, "recall the detail"))
	t.Logf("Handle error=%v deletion=%v", err, deleted)
	require.True(t, deleted)
	var count int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='alice' AND update_id=99101 AND kind='reply' AND content::text LIKE '%'||$1||'%'`, canary).
			Scan(&count),
	)
	require.NoError(
		t,
		f.b.Render(t.Context(), "alice", 101),
	) // A later explicit render occurs after deletion and Handle return.
	response, err := http.Get(f.fake.URL + "/lab/state?user=101")
	require.NoError(t, err)
	defer response.Body.Close()
	var state liveState
	require.NoError(t, json.NewDecoder(response.Body).Decode(&state))
	raw, err := json.Marshal(state)
	require.NoError(t, err)
	t.Logf("persisted stale replies=%d Telegram state=%s", count, raw)
	require.NotContains(t, string(raw), canary, "deletion completed before interaction insertion and rendering")
	require.Zero(t, count)
}
