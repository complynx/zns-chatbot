package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Rewrite only the synthetic upstream update; the host still decodes ordinary
// Telegram JSON and persists/processes it through Bot.Run.
type fullHistoryUpdateTransport struct{ text string }

func (tr fullHistoryUpdateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil || !strings.HasSuffix(request.URL.Path, "/getUpdates") {
		return response, err
	}
	var payload struct {
		OK     bool              `json:"ok"`
		Result []telegram.Update `json:"result"`
	}
	err = json.NewDecoder(response.Body).Decode(&payload)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	for index := range payload.Result {
		if msg := payload.Result[index].Message; msg != nil && msg.Text == "full-history-marker" {
			msg.Text = tr.text
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	response.ContentLength = int64(len(raw))
	return response, nil
}

func TestLiveTelegramHistoryRetainsCompleteUnicode(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		strings.Repeat("Ж🙂漢字é e\u0301\n\t\"\\", 250) + "USER-END",
		strings.Repeat("Ж", 2499) + "🙂BOUNDARY-END",
	} {
		t.Run(source[len(source)-8:], func(t *testing.T) {
			t.Parallel()
			checkLiveHistoryRetention(t, source)
		})
	}
}

func checkLiveHistoryRetention(t *testing.T, source string) {
	t.Helper()
	f := setup(t)
	f.b.TG.HTTP = &http.Client{Transport: fullHistoryUpdateTransport{text: source}}
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		assert.LessOrEqual(t, len(input.Text), 5000)
		assert.True(t, utf8.ValidString(input.Text))
		return agent.Plan{View: "workflow", Text: "Retained reply"}, nil
	})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "full-history-marker"})
	completeInbox(t, f, 2)
	var id int64
	var body string
	var omitted bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT e.id,e.omitted,COALESCE(b.body,e.text) FROM core.conversation_events e LEFT JOIN core.conversation_message_bodies b ON b.event_id=e.id WHERE e.owner='alice' AND e.source_key='tg-user-1'`).
			Scan(&id, &omitted, &body),
	)
	require.False(t, omitted)
	require.Equal(t, source, body)
	restarted := *f.b
	f.b = &restarted
	completeInbox(t, f, 2)
	window, err := f.b.API.ConversationWindow(t.Context(), "alice", conversation.DefaultRecent)
	require.NoError(t, err)
	var found bool
	for _, event := range window.Recent {
		if event.ID == id {
			found = true
			assert.True(t, event.HasFullText)
			assert.LessOrEqual(t, len(event.Text), conversation.MaxTextBytes)
			assert.True(t, utf8.ValidString(event.Text))
		}
	}
	require.True(t, found)
	archive := conversation.Service{DB: f.db}
	var rebuilt strings.Builder
	var digest string
	for offset := 0; ; {
		chunk, readErr := archive.ReadText(t.Context(), "alice", id, offset, 503, digest)
		require.NoError(t, readErr)
		assert.Equal(t, utf8.RuneCountInString(source), chunk.Total)
		digest = chunk.Digest
		rebuilt.WriteString(chunk.Text)
		if !chunk.More {
			break
		}
		offset = chunk.NextOffset
	}
	assert.Equal(t, source, rebuilt.String())
	_, err = archive.ReadText(t.Context(), "bob", id, 0, 503, "")
	require.Error(t, err)
	require.NoError(t, archive.DeleteContent(t.Context(), "alice", id))
	require.NoError(t, archive.Append(t.Context(), "alice", "tg-user-1", "user", source))
	chunk, err := archive.ReadText(t.Context(), "alice", id, 0, 503, "")
	require.NoError(t, err)
	assert.True(t, chunk.Omitted)
	assert.Empty(t, chunk.Text)
	var bodies int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_message_bodies WHERE event_id=$1`, id).
			Scan(&bodies),
	)
	assert.Zero(t, bodies, "same source retry must not resurrect a deleted body")
}

func TestLiveTelegramHistorySuppressesSensitiveSuffix(t *testing.T) {
	t.Parallel()
	f := setup(t)
	source := strings.Repeat("Ж", 2600) + " password: suffix-private"
	f.b.TG.HTTP = &http.Client{Transport: fullHistoryUpdateTransport{text: source}}
	f.b.Model = avModel(func(_ context.Context, input agent.Input) (agent.Plan, error) {
		assert.LessOrEqual(t, len(input.Text), 5000)
		raw, err := json.Marshal(input)
		if err != nil {
			return agent.Plan{}, err
		}
		assert.NotContains(t, string(raw), "suffix-private")
		return agent.Plan{View: "workflow", Text: "response-private-canary"}, nil
	})
	post(t, f.fake.URL+"/lab/input", map[string]any{"user": 101, "text": "full-history-marker"})
	completeInbox(t, f, 2)
	var requestText, replyText string
	var omitted bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT text,omitted FROM core.conversation_events WHERE owner='alice' AND source_key='tg-user-1'`).
			Scan(&requestText, &omitted),
	)
	assert.True(t, omitted)
	assert.Equal(t, "[sensitive text omitted]", requestText)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT text FROM core.conversation_events WHERE owner='alice' AND source_key='tg-assistant-1'`).
			Scan(&replyText),
	)
	assert.Equal(t, "[response to sensitive request omitted]", replyText)
	var bodies int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.conversation_message_bodies`).Scan(&bodies),
	)
	assert.Zero(t, bodies)
}
