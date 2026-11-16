package loom_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestProxyFailureDoesNotProveRunOrMessageRejection(t *testing.T) {
	t.Parallel()
	for _, status := range []int{400, 403, 404, 409, 422} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "404 page not found\n")
			}))
			defer srv.Close()
			c, err := loom.New(srv.URL, srv.Client())
			require.NoError(t, err)
			auth := loom.Auth{ServiceToken: "actor", SubjectToken: "user"}
			_, err = c.GetRun(context.Background(), auth, "retained-run")
			var failure *loom.Error
			require.ErrorAs(t, err, &failure)
			require.True(t, failure.Retryable)
			_, err = c.RecoverMessage(context.Background(), auth, "chat", "immutable-key", loom.RecoverMessage{Text: "hello"})
			require.ErrorAs(t, err, &failure)
			require.True(t, failure.Retryable)
			require.Equal(t, loom.Unknown, failure.Outcome)
		})
	}
}

func TestRecoveryWireReplaysFrozenBodyAfterLostReceipt(t *testing.T) {
	t.Parallel()
	var first []byte
	calls := 0
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/v1/capabilities":
			_, _ = io.WriteString(w, `{"conversation_recovery":{"version":1}}`)
		case "/v1/chats/default":
			_, _ = io.WriteString(w, `{"id":"chat"}`)
		case "/v1/chats/chat/active-run":
			_, _ = io.WriteString(w, `{"run_id":"run","status":"accepted","next_action":"send_message"}`)
		case "/v1/chats/chat/messages":
			assert.Equal(t, "immutable-key", r.Header.Get("Idempotency-Key"))
			assert.Equal(t, "user", r.Header.Get("X-Subject-Token"))
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			calls++
			if calls == 1 {
				first = body
				conn, _, hijackErr := w.(http.Hijacker).Hijack()
				if !assert.NoError(t, hijackErr) {
					return
				}
				_ = conn.Close()
				return
			}
			assert.Equal(t, first, body)
			_ = json.NewEncoder(w).
				Encode(loom.RecoveryReceipt{Accepted: true, Delivered: true, MessageID: "message",
					Sequence: 3, RunID: "successor", Status: "queued", NextAction: "read_run"})
		default:
			t.Errorf("unexpected route %s", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	c, err := loom.New(srv.URL, srv.Client())
	require.NoError(t, err)
	auth := loom.Auth{ServiceToken: "actor", SubjectToken: "user"}
	ctx := context.Background()
	supported, err := c.SupportsConversationRecovery(ctx, auth)
	require.NoError(t, err)
	require.True(t, supported)
	chat, err := c.DefaultChat(ctx, auth)
	require.NoError(t, err)
	require.Equal(t, "chat", chat)
	active, err := c.ActiveRun(ctx, auth, chat)
	require.NoError(t, err)
	require.Equal(t, "run", active.RunID)
	input := loom.RecoverMessage{Text: "Continue", ReplaceRunID: active.RunID}
	_, err = c.RecoverMessage(ctx, auth, chat, "immutable-key", input)
	var failure *loom.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, loom.Unknown, failure.Outcome)
	receipt, err := c.RecoverMessage(ctx, auth, chat, "immutable-key", input)
	require.NoError(t, err)
	require.Equal(t, "successor", receipt.RunID)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 2, calls)
}

func TestAbandonMessageRequiresItsMatchingUndeliveredReceipt(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"matching", "foreign", "delivered"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/chats/chat/messages/message/abandon", r.URL.Path)
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "withdraw-key", r.Header.Get("Idempotency-Key"))
				assert.Equal(t, "user", r.Header.Get("X-Subject-Token"))
				receipt := loom.RecoveryReceipt{Accepted: true, Abandoned: true, MessageID: "message"}
				if mode == "foreign" {
					receipt.MessageID = "other-message"
				}
				receipt.Delivered = mode == "delivered"
				respondErr := json.NewEncoder(w).Encode(receipt)
				assert.NoError(t, respondErr)
			}))
			defer srv.Close()
			client, err := loom.New(srv.URL, srv.Client())
			require.NoError(t, err)
			_, err = client.AbandonMessage(t.Context(), loom.Auth{ServiceToken: "actor", SubjectToken: "user"},
				"chat", "message", "withdraw-key")
			if mode == "matching" {
				require.NoError(t, err)
				return
			}
			var failure *loom.Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, loom.Unknown, failure.Outcome)
		})
	}
}
