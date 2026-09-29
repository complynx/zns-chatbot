package appclient_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

func TestHistoryHTTPDecodeFailuresReturnZero(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, body string
		read       func(appclient.Client) (any, error)
	}{
		{"window", `{"generation":7,"recent":[{"id":1,"text":"private"}]} false`, func(c appclient.Client) (any, error) { return c.ConversationWindow(t.Context(), "alice", 1) }},
		{"page", `{"generation":7,"events":[{"id":1,"text":"private"}]} false`, func(c appclient.Client) (any, error) {
			return c.ConversationHistory(t.Context(), "alice", conversation.Query{Limit: 1})
		}},
		{"generation", `{"generation":7} false`, func(c appclient.Client) (any, error) { return c.HistoryGeneration(t.Context(), "alice") }},
		{"text", `{"event_id":1,"text":"private"} false`, func(c appclient.Client) (any, error) { return c.ConversationText(t.Context(), "alice", 1, 0, 1, "") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(
				http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, test.body) }),
			)
			t.Cleanup(server.Close)
			value, err := test.read(
				appclient.Client{
					Base:         server.URL,
					HTTP:         server.Client(),
					SandboxToken: func(string) string { return "token" },
				},
			)
			require.Error(t, err)
			require.Empty(t, value)
		})
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[{"id":1,"text":"private"}] false`)
	}))
	t.Cleanup(server.Close)
	host := appclient.Host{
		Base:      server.URL,
		HTTP:      server.Client(),
		UserToken: func(context.Context, string) (string, error) { return "token", nil },
	}
	events, err := host.HistorySummaryBatch(t.Context(), "alice", 1)
	require.Error(t, err)
	require.Empty(t, events)
}
