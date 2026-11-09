package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type webhookIntake struct{ accepting atomic.Bool }

func (i *webhookIntake) SetAccepting(value bool) { i.accepting.Store(value) }

func TestWebhookStartupDisabledDoesNotContactTelegram(t *testing.T) {
	t.Parallel()
	require.NoError(t, synchronizeWebhook(t.Context(), config.Config{}))
}

func TestWebhookStartupReassertsSecretAndPreservesPending(t *testing.T) {
	t.Parallel()
	requests := make(chan telegram.SetWebhookRequest, 2)
	intake := &webhookIntake{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		switch {
		case strings.HasSuffix(r.URL.Path, "/setWebhook"):
			var input telegram.SetWebhookRequest
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
			assert.False(t, intake.accepting.Load())
			requests <- input
			fmt.Fprint(w, `{"ok":true,"result":true}`)
		case strings.HasSuffix(r.URL.Path, "/getWebhookInfo"):
			fmt.Fprint(w, `{"ok":true,"result":{"url":"https://bot.invalid/telegram/webhook",`+
				`"allowed_updates":["message","callback_query"],"pending_update_count":17}}`)
		default:
			t.Error("unexpected Telegram method")
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	c := config.Config{MiniAppOrigin: "https://bot.invalid", HTTPTimeout: time.Second,
		Telegram: config.Telegram{WebhookEnabled: true, BaseURL: server.URL, Token: "123:private_token",
			WebhookSecret: "first_secret"}}
	require.NoError(t, readyApplication(t.Context(), c, intake, slog.New(slog.DiscardHandler)))
	require.True(t, intake.accepting.Load())
	intake.SetAccepting(false)
	c.Telegram.WebhookSecret = "rotated_secret"
	require.NoError(t, readyApplication(t.Context(), c, intake, slog.New(slog.DiscardHandler)))
	require.True(t, intake.accepting.Load())
	intake.SetAccepting(false)
	require.Len(t, requests, 2)
	first, second := <-requests, <-requests
	require.Equal(t, "first_secret", first.SecretToken)
	require.Equal(t, "rotated_secret", second.SecretToken)
	for _, r := range []telegram.SetWebhookRequest{first, second} {
		require.False(t, r.DropPendingUpdates)
		require.Equal(t, "https://bot.invalid/telegram/webhook", r.URL)
		require.ElementsMatch(t, []string{"message", "callback_query"}, r.AllowedUpdates)
	}
}

func TestWebhookFailureKeepsIntakeClosedAndErrorsPrivate(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"rejected", "false", "lost", "readback", "updates", "read_error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var sets atomic.Int32
			server := httptest.NewServer(failingWebhookHandler(t, mode, &sets))
			defer server.Close()
			c := config.Config{MiniAppOrigin: "https://bot.invalid", HTTPTimeout: time.Second,
				Telegram: config.Telegram{WebhookEnabled: true, BaseURL: server.URL, Token: "123:private_token",
					WebhookSecret: "private_secret"}}
			intake := &webhookIntake{}
			err := readyApplication(t.Context(), c, intake, slog.New(slog.DiscardHandler))
			require.Error(t, err)
			require.False(t, intake.accepting.Load())
			require.Equal(t, int32(1), sets.Load())
			require.NotContains(t, err.Error(), "private_token")
			require.NotContains(t, err.Error(), "private_secret")
		})
	}
}

func failingWebhookHandler(t *testing.T, mode string, sets *atomic.Int32) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setWebhook") {
			sets.Add(1)
			webhookSetResponse(t, w, mode)
			return
		}
		if mode == "read_error" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		url := "https://bot.invalid/telegram/webhook"
		updates := []string{"message", "callback_query"}
		if mode == "readback" {
			url = "https://wrong.invalid/telegram/webhook"
		}
		if mode == "updates" {
			updates = []string{"message", "edited_message"}
		}
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"ok": true,
			"result": telegram.WebhookInfo{URL: url, AllowedUpdates: updates}}))
	}
}

func webhookSetResponse(t *testing.T, w http.ResponseWriter, mode string) {
	t.Helper()
	switch mode {
	case "rejected":
		fmt.Fprint(w, `{"ok":false,"error_code":401,"description":"private_token"}`)
	case "false":
		fmt.Fprint(w, `{"ok":true,"result":false}`)
	case "lost":
		conn, _, err := w.(http.Hijacker).Hijack()
		if assert.NoError(t, err) {
			assert.NoError(t, conn.Close())
		}
	default:
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}
}

func TestWebhookStartupTimeoutKeepsIntakeClosed(t *testing.T) {
	t.Parallel()
	c := config.Config{MiniAppOrigin: "https://bot.invalid", HTTPTimeout: time.Nanosecond,
		Telegram: config.Telegram{WebhookEnabled: true, BaseURL: "http://127.0.0.1:1", Token: "123:private_token"}}
	intake := &webhookIntake{}
	require.Error(t, readyApplication(t.Context(), c, intake, slog.New(slog.DiscardHandler)))
	require.False(t, intake.accepting.Load())
}
