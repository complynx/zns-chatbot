package loom_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestReactionHistoryUsesChatSequenceAndRejectsInvalidPages(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"events":[{"seq":8,"type":"message.user","reactions_seq":15,"reactions":{"assistant":["🙈"]}}]}`,
		`{"events":[{"seq":7}]}`, `{"events":[{"seq":9},{"seq":8}]}`,
		`{"events":[{"seq":8,"reactions_seq":-1}]}`,
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/v1/chats/owned/history?after=7&limit=2", r.URL.RequestURI())
				assert.Equal(t, "Bearer actor", r.Header.Get("Authorization"))
				assert.Equal(t, "subject", r.Header.Get("X-Subject-Token"))
				_, _ = fmt.Fprint(w, body)
			}))
			defer server.Close()
			client, err := loom.New(server.URL, server.Client())
			require.NoError(t, err)
			events, err := client.ChatHistory(t.Context(), loom.Auth{ServiceToken: "actor", SubjectToken: "subject"},
				"owned", 7, 2)
			if body == `{"events":[{"seq":8,"type":"message.user","reactions_seq":15,"reactions":{"assistant":["🙈"]}}]}` {
				require.NoError(t, err)
				require.EqualValues(t, 15, events[0].ReactionsSeq)
			} else {
				require.Error(t, err)
			}
		})
	}
}
