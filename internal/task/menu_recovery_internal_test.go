package task

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func menuHTTPService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := telegram.New(server.URL, "123:private_token", server.Client())
	require.NoError(t, err)
	return &Service{tg: client}
}

func TestMenuRateLimitPausesAllRecipients(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests",`+
				`"parameters":{"retry_after":90}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	require.Error(t, s.applyMenu(t.Context(), 42, nil, nil))
	require.Greater(t, s.menuPauseDelay(), 80*time.Second)
	s.resetMenuRetry(42)
	require.NoError(t, s.applyMenu(t.Context(), 42, nil, nil))
	require.NoError(t, s.applyMenu(t.Context(), 43, nil, nil))
	require.Equal(t, int32(1), calls.Load(), "interaction cannot bypass bot-wide RetryAfter")
	s.menuPausedUntil = time.Now().Add(-time.Second)
	require.NoError(t, s.applyMenu(t.Context(), 43, nil, nil))
	require.Equal(t, int32(4), calls.Load(), "other recipients recover when quota permits")
	require.NoError(t, s.applyMenu(t.Context(), 42, nil, nil))
	require.Equal(t, int32(7), calls.Load())
}

func TestMenuRejectedChatDoesNotBlockOthersAndInteractionRetries(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := menuHTTPService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body struct {
			ChatID int64 `json:"chat_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.HasSuffix(r.URL.Path, "/setChatMenuButton") && body.ChatID == 42 {
			fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	require.Error(t, s.applyMenu(t.Context(), 42, nil, nil))
	require.Greater(t, time.Until(s.menuRetry[42].until), 50*time.Minute)
	require.NoError(t, s.applyMenu(t.Context(), 42, nil, nil))
	require.Equal(t, int32(3), calls.Load())
	require.NoError(t, s.applyMenu(t.Context(), 43, nil, nil))
	require.Equal(t, int32(6), calls.Load())
	s.resetMenuRetry(42)
	require.Error(t, s.applyMenu(t.Context(), 42, nil, nil))
	require.Equal(t, int32(9), calls.Load(), "fresh interaction retries a previously unreachable chat")
}

func TestPartialMenuFailureInvalidatesOldSuccess(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 5 {
			fmt.Fprint(w, `{"ok":false,"error_code":500,"description":"Unavailable"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	old := startCommands(false, db.TelegramMenuAvailability{}, true)
	next := startCommands(false, db.TelegramMenuAvailability{Register: true}, true)
	require.NoError(t, s.applyMenu(t.Context(), 42, old, old))
	require.Error(t, s.applyMenu(t.Context(), 42, next, next))
	require.NoError(t, s.applyMenu(t.Context(), 42, old, old))
	require.Equal(t, int32(8), calls.Load(), "old cache cannot hide partially changed remote commands")
}

func TestMenuDiagnosticsExposeOnlySafeFailureFields(t *testing.T) {
	t.Parallel()
	s := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"ok":false,"error_code":400,"description":"private_person private_token"}`)
	})
	var output bytes.Buffer
	s.logger = slog.New(slog.NewJSONHandler(&output, nil))
	err := s.applyMenu(t.Context(), 42, nil, nil)
	require.Error(t, err)
	s.warnMenuFailure(t.Context(), err)
	var event map[string]any
	decoder := json.NewDecoder(bytes.NewReader(output.Bytes()))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&event))
	require.Equal(t, "set_commands_default", event["operation"])
	require.Equal(t, json.Number("400"), event["status"])
	require.Equal(t, "rejected", event["outcome"])
	require.Equal(t, "other_rejection", event["reason"])
	require.NotContains(t, output.String(), "private_person")
	require.NotContains(t, output.String(), "private_token")
	require.NotContains(t, output.String(), "chat_id")
}

func TestFallbackMenuClearingHonorsRateLimit(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"Too Many Requests",`+
				`"parameters":{"retry_after":180}}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	})
	require.False(t, s.clearFallbackMenus(t.Context()))
	require.Equal(t, int32(1), calls.Load())
	require.Greater(t, s.menuPauseDelay(), 170*time.Second)
	s.menuPausedUntil = time.Now().Add(-time.Second)
	require.True(t, s.clearFallbackMenus(t.Context()))
	require.Equal(t, int32(5), calls.Load())
}
