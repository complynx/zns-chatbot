package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestRegisterTelegramMenuExactParityAndRetry(t *testing.T) {
	t.Parallel()
	for _, failAt := range []int{0, 1, 2, 3} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			t.Parallel()
			var methods []string
			var requests []map[string]json.RawMessage
			fail := failAt
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request map[string]json.RawMessage
				if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&request)) {
					return
				}
				methods = append(methods, r.URL.Path)
				requests = append(requests, request)
				w.Header().Set("Content-Type", "application/json")
				if fail > 0 && len(methods) == fail {
					_, err := w.Write([]byte(`{"ok":false,"error_code":429,"description":"retry"}`))
					assert.NoError(t, err)
					return
				}
				_, err := w.Write([]byte(`{"ok":true,"result":true}`))
				assert.NoError(t, err)
			}))
			t.Cleanup(server.Close)
			b := Bot{TG: telegram.Client{Base: server.URL, Token: "synthetic"}}
			err := b.registerTelegramMenu(t.Context())
			if failAt > 0 {
				require.Error(t, err)
				assert.Len(t, methods, failAt)
			} else {
				require.NoError(t, err)
				assert.Len(t, methods, 3)
			}
			methods = nil
			requests = nil
			fail = 0
			require.NoError(t, b.registerTelegramMenu(t.Context()))
			assert.Equal(
				t,
				[]string{
					"/botsynthetic/setChatMenuButton",
					"/botsynthetic/setMyCommands",
					"/botsynthetic/setMyCommands",
				},
				methods,
			)
			require.Len(t, requests, 3)
			assert.JSONEq(t, `{"type":"commands"}`, string(requests[0]["menu_button"]))
			assert.NotContains(t, requests[0], "chat_id")
			assert.NotContains(t, requests[1], "language_code")
			assert.NotContains(t, requests[1], "scope")
			assert.JSONEq(t, `"ru"`, string(requests[2]["language_code"]))
			assert.JSONEq(
				t,
				`[{"command":"passes","description":"Register to ZNS or manage your registration"},{"command":"massage","description":"Book a massage"},{"command":"orders","description":"Order food, transportation and activities."}]`,
				string(requests[1]["commands"]),
			)
			assert.JSONEq(
				t,
				`[{"command":"passes","description":"Регистрация на ЗНС и управление ей"},{"command":"massage","description":"Запись на массаж"},{"command":"orders","description":"Заказ еды, транспорта и активностей."}]`,
				string(requests[2]["commands"]),
			)
		})
	}
}

func TestRegisterTelegramMenuCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	server := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { cancel(); <-ctx.Done() }),
	)
	t.Cleanup(server.Close)
	b := Bot{TG: telegram.Client{Base: server.URL, Token: "synthetic"}}
	require.Error(t, b.registerTelegramMenu(ctx))
	assert.ErrorIs(t, ctx.Err(), context.Canceled)
}
