package telegram_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestSendMessageAndCallback(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		switch r.URL.Path {
		case "/bot123:secret/sendMessage":
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":7,"chat":{"id":42}}}`)
		case "/bot123:secret/answerCallbackQuery":
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		default:
			t.Errorf("unexpected method")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	receipt, err := client.SendMessage(context.Background(), telegram.SendMessageRequest{ChatID: 42, Text: "Привет 👋"})
	require.NoError(t, err)
	require.Equal(t, int64(7), receipt.MessageID)
	require.Equal(t, int64(42), receipt.ChatID)
	require.NoError(t, client.AnswerCallbackQuery(context.Background(), "callback", "Accepted"))
}
func TestLostSendReceiptIsUnknownAndSafe(t *testing.T) {
	t.Parallel()
	var accepted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		accepted.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if !assert.NoError(t, err) {
			return
		}
		assert.NoError(t, conn.Close())
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	_, err = client.SendMessage(context.Background(), telegram.SendMessageRequest{ChatID: 42, Text: "hello"})
	var failure *telegram.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, telegram.Unknown, failure.Outcome)
	require.False(t, failure.Retryable)
	require.Equal(t, int32(1), accepted.Load())
	require.NotContains(t, err.Error(), "secret")
	require.NotContains(t, err.Error(), server.URL)
}
func TestRateLimitAndBoundedResponse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		status     int
		outcome    telegram.Outcome
		retry      bool
	}{
		{"rate-limit", `{"ok":false,"error_code":429,"parameters":{"retry_after":3},"description":"secret"}`, 429, telegram.Rejected, true},
		{"oversized", strings.Repeat("x", telegram.MaxResponseBytes+1), 200, telegram.Unknown, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(
				http.HandlerFunc(
					func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) },
				),
			)
			defer server.Close()
			client, err := telegram.New(server.URL, "123:secret", server.Client())
			require.NoError(t, err)
			_, err = client.SendMessage(context.Background(), telegram.SendMessageRequest{ChatID: 42, Text: "hello"})
			var failure *telegram.Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.outcome, failure.Outcome)
			require.Equal(t, tc.retry, failure.Retryable)
			if tc.retry {
				require.Equal(t, 3*time.Second, failure.RetryAfter)
			}
			require.NotContains(t, err.Error(), "secret")
		})
	}
}
func TestRedirectDoesNotExposeToken(t *testing.T) {
	t.Parallel()
	var reached atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached.Store(true) }))
	defer target.Close()
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
		}),
	)
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	_, err = client.SendMessage(context.Background(), telegram.SendMessageRequest{ChatID: 42, Text: "hello"})
	require.Error(t, err)
	require.False(t, reached.Load())
}

func TestMalformedSuccessDoesNotConfirmDelivery(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"ok":true,"result":{}}`) }),
	)
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	_, err = client.SendMessage(context.Background(), telegram.SendMessageRequest{ChatID: 42, Text: "hello"})
	var failure *telegram.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, telegram.Unknown, failure.Outcome)
	require.False(t, failure.Retryable)
}
func TestCancelledBeforeDispatchDoesNotSend(t *testing.T) {
	t.Parallel()
	var reached atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { reached.Store(true) }))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.SendMessage(ctx, telegram.SendMessageRequest{ChatID: 42, Text: "hello"})
	var failure *telegram.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, telegram.NotStarted, failure.Outcome)
	require.False(t, reached.Load())
}

func TestEntityFallbackOnlyForExplicitRejection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, description string
		invalid           bool
	}{
		{"entities", "Bad Request: can't parse entities: unsupported nesting", true},
		{"generic", "Bad Request: chat not found", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, `{"ok":false,"error_code":400,"description":%q}`, tc.description)
			}))
			defer server.Close()
			client, err := telegram.New(server.URL, "123:secret", server.Client())
			require.NoError(t, err)
			_, err = client.SendMessage(context.Background(), telegram.SendMessageRequest{ChatID: 42, Text: "hello"})
			var failure *telegram.Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.invalid, failure.InvalidEntities)
			require.Equal(t, telegram.Rejected, failure.Outcome)
			require.NotContains(t, err.Error(), tc.description)
		})
	}
}

func TestIncompleteEnvelopeNeverConfirmsMutationOutcome(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"send", "edit"} {
		for _, tc := range []struct {
			name, body string
			outcome    telegram.Outcome
		}{
			{"missing-ok", `{}`, telegram.Unknown},
			{"missing-ok-with-result", `{"result":{"message_id":7,"chat":{"id":42}}}`, telegram.Unknown},
			{"incomplete-rejection", `{"ok":false}`, telegram.Unknown},
			{"explicit-rejection", `{"ok":false,"error_code":400}`, telegram.Rejected},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				server := httptest.NewServer(
					http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, tc.body) }),
				)
				defer server.Close()
				client, err := telegram.New(server.URL, "123:secret", server.Client())
				require.NoError(t, err)
				if method == "send" {
					_, err = client.SendMessage(
						context.Background(),
						telegram.SendMessageRequest{ChatID: 42, Text: "hello"},
					)
				} else {
					_, err = client.EditMessageText(
						context.Background(),
						telegram.EditMessageRequest{ChatID: 42, MessageID: 7, Text: "hello"},
					)
				}
				var failure *telegram.Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tc.outcome, failure.Outcome)
				require.False(t, failure.Retryable)
				require.NotContains(t, err.Error(), tc.body)
			})
		}
	}
}

func TestClosedListenerDoesNotStartDelivery(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	endpoint := "http://" + listener.Addr().String()
	require.NoError(t, listener.Close())
	client, err := telegram.New(endpoint, "123:secret", &http.Client{Timeout: time.Second})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = client.SendMessage(ctx, telegram.SendMessageRequest{ChatID: 42, Text: "hello"})
	var failure *telegram.Error
	require.ErrorAs(t, err, &failure)
	require.Equal(t, telegram.NotStarted, failure.Outcome)
	require.True(t, failure.Retryable)
	require.NotContains(t, err.Error(), endpoint)
	require.NotContains(t, err.Error(), "secret")
}
