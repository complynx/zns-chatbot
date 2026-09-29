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

type qaHistoryDeleteTransport struct{ before func(*http.Request) error }

func (tr qaHistoryDeleteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := tr.before(r); err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestQAHistoryDeletionDuringReplyArchive(t *testing.T) {
	t.Parallel()
	f := setup(t)
	archive := conversation.Service{DB: f.db}
	const canary = "ordinary violet lighthouse historical detail"
	require.NoError(t, archive.Append(t.Context(), "alice", "qa-history-source", "user", canary))
	var id int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='qa-history-source'`).
			Scan(&id),
	)
	f.b.Model = avModel(func(_ context.Context, in agent.Input) (agent.Plan, error) {
		raw, err := json.Marshal(in)
		require.NoError(t, err)
		require.Contains(t, string(raw), canary)
		return agent.Plan{View: "workflow", Text: canary}, nil
	})
	deleted := false
	f.b.API.HTTP = &http.Client{Transport: qaHistoryDeleteTransport{before: func(r *http.Request) error {
		if r.URL.Path != "/internal/history/archive" {
			return nil
		}
		copyBody, err := r.GetBody()
		if err != nil {
			return err
		}
		defer copyBody.Close()
		var payload struct {
			Kind string `json:"kind"`
		}
		if err = json.NewDecoder(copyBody).Decode(&payload); err != nil {
			return err
		}
		if payload.Kind == "assistant" && !deleted {
			deleted = true
			return archive.DeleteContent(r.Context(), "alice", id)
		}
		return nil
	}}}
	err := f.b.Handle(t.Context(), message(9801, 101, "recall the previous detail"))
	t.Logf("Handle error=%v deletion_triggered=%v", err, deleted)
	require.True(t, deleted)
	var retained int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_events WHERE owner='alice' AND text LIKE '%'||$1||'%'`, canary).
			Scan(&retained),
	)
	require.Zero(t, retained, "a reply derived before deletion must not become a new history event after deletion")
}
