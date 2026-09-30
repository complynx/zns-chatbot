package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type legacyOrderReplyRequest struct {
	Owner     string                   `json:"Owner"`
	Chat      int64                    `json:"Chat"`
	Update    int64                    `json:"Update"`
	Effect    string                   `json:"Effect"`
	Reference botdelivery.Reference    `json:"Reference"`
	Result    botdelivery.StoredResult `json:"Result"`
	Target    int64                    `json:"Target"`
}

func TestLegacyOrderPendingExportDoesNotQueueEmptyReply(t *testing.T) {
	t.Parallel()
	// No database or Host is needed when the document is still pending.
	var b Bot
	err := b.queueLegacyOrderReply(t.Context(), incoming{owner: "bob", chat: 202}, telegram.Update{ID: 7401}, "")
	require.NoError(t, err)
}

func TestLegacyOrderNonemptyReplyRetainsCallbackTarget(t *testing.T) {
	t.Parallel()
	b, queries := sqlBot(t, nil, pgReply{action: pgRows}, pgReply{action: pgRows}, pgReply{action: pgRows})
	requests := make(chan legacyOrderReplyRequest, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/internal/bot-delivery/result", r.URL.Path)
		var request legacyOrderReplyRequest
		if err := json.NewDecoder(r.Body).Decode(&request); !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests <- request
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	b.Host = appclient.Host{Base: server.URL, UserToken: func(context.Context, string) (string, error) {
		return "synthetic", nil
	}}
	update := telegram.Update{ID: 7401, Callback: &telegram.Callback{Message: telegram.Message{ID: 19}}}
	notices := []string{"Export delivered", "Export delivered", "forbidden"}
	var effects []string
	for _, notice := range notices {
		require.NoError(t, b.queueLegacyOrderReply(t.Context(), incoming{owner: "bob", chat: 202}, update, notice))
		var request legacyOrderReplyRequest
		select {
		case request = <-requests:
		default:
			t.Fatal("nonempty callback feedback was not queued")
		}
		assert.Equal(t, "bob", request.Owner)
		assert.Equal(t, int64(202), request.Chat)
		assert.Equal(t, int64(19), request.Target)
		assert.Equal(t, update.ID, request.Update)
		assert.Contains(t, request.Effect, "legacy_order_reply:")
		assert.Equal(t, notice, request.Result.Payload.Text)
		assert.Equal(t, "legacy_order", request.Reference.Family)
		effects = append(effects, request.Effect)
		select {
		case query := <-queries:
			assert.Contains(t, query, "INSERT INTO bot.interactions")
		default:
			t.Fatal("callback feedback was not recorded")
		}
	}
	assert.Equal(t, effects[0], effects[1], "identical callback feedback keeps its delivery identity")
	assert.NotEqual(t, effects[0], effects[2], "current refusal must not replay successful feedback")
}
