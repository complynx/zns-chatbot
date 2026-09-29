package sandbox

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestTelegramMenuStateFailureAndIdempotency(t *testing.T) {
	t.Parallel()
	fake := &Fake{Token: "synthetic", fault: transientFault}
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "synthetic"}
	commands := []telegram.BotCommand{{Command: "passes", Description: "Registration"}}
	require.NoError(t, client.SetDefaultCommandsMenu(t.Context()))
	require.NoError(t, client.SetMyCommands(t.Context(), "", commands))
	request := httptest.NewRequest(
		http.MethodPost,
		"/lab/menu-fault",
		strings.NewReader(`{"method":"setMyCommands","language_code":"ru"}`),
	)
	request.Header.Set("X-Sandbox", "1")
	response := httptest.NewRecorder()
	fake.Handler().ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.Error(t, client.SetMyCommands(t.Context(), "ru", commands))
	require.NoError(t, client.SetMyCommands(t.Context(), "ru", commands))
	require.NoError(t, client.SetMyCommands(t.Context(), "ru", commands))
	stateResponse := httptest.NewRecorder()
	fake.Handler().ServeHTTP(stateResponse, httptest.NewRequest(http.MethodGet, "/lab/telegram-menu", nil))
	var state telegramMenuState
	require.NoError(t, json.Unmarshal(stateResponse.Body.Bytes(), &state))
	require.NotNil(t, state.Button)
	assert.Equal(t, "commands", state.Button.Type)
	assert.Equal(t, commands, state.Commands[""])
	assert.Equal(t, commands, state.Commands["ru"])
	assert.EqualValues(t, 5, state.TotalCalls)
	require.Len(t, state.Calls, 5)
	assert.False(t, state.Calls[2].Applied)
	fake.mu.Lock()
	assert.Equal(t, transientFault, fake.fault, "menu setup must not consume existing message fault")
	fake.mu.Unlock()
	var read []telegram.BotCommand
	require.NoError(t, client.Call(t.Context(), "getMyCommands", map[string]string{"language_code": "ru"}, &read))
	assert.Equal(t, commands, read)
}

func TestTelegramMenuRejectsUnsupportedScopeAndInvalidCommands(t *testing.T) {
	t.Parallel()
	fake := &Fake{Token: "synthetic"}
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "synthetic"}
	for _, payload := range []string{
		`{"commands":[{"command":"BAD","description":"invalid"}]}`,
		`{"commands":[{"command":"passes","description":""}]}`,
		`{"scope":{"type":"all_private_chats"},"commands":[]}`,
		`{"language_code":"russian","commands":[]}`,
	} {
		require.Error(t, client.Call(t.Context(), setCommandsMethod, json.RawMessage(payload), nil))
	}
	require.Error(
		t,
		client.Call(
			t.Context(),
			setMenuButtonMethod,
			json.RawMessage(`{"chat_id":101,"menu_button":{"type":"commands"}}`),
			nil,
		),
	)
	fake.mu.Lock()
	assert.Zero(t, fake.menu.TotalCalls)
	fake.mu.Unlock()
}
