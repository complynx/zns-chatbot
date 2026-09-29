package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type registeredMenu struct {
	TotalCalls int                              `json:"total_calls"`
	Button     telegram.MenuButton              `json:"menu_button"`
	Commands   map[string][]telegram.BotCommand `json:"commands"`
	Calls      []struct {
		Method   string `json:"method"`
		Language string `json:"language_code"`
		Applied  bool   `json:"applied"`
	} `json:"recent_calls"`
}

func readRegisteredMenu(t *testing.T, url string) registeredMenu {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url+"/lab/telegram-menu", nil)
	require.NoError(t, err)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var state registeredMenu
	require.NoError(t, json.NewDecoder(response.Body).Decode(&state))
	return state
}

func TestTelegramMenuExclusiveStartupAndRestart(t *testing.T) {
	t.Parallel()
	f := setup(t)
	connection, err := f.db.Acquire(t.Context())
	require.NoError(t, err)
	defer connection.Release()
	_, err = connection.Exec(t.Context(), `SELECT pg_advisory_lock(918273)`)
	require.NoError(t, err)
	require.ErrorContains(t, f.b.Run(t.Context()), "bot already running")
	assert.Zero(t, readRegisteredMenu(t, f.fake.URL).TotalCalls)
	_, err = connection.Exec(t.Context(), `SELECT pg_advisory_unlock(918273)`)
	require.NoError(t, err)
	runInboxUntil(t, f, func() bool { return readRegisteredMenu(t, f.fake.URL).TotalCalls == 3 })
	first := readRegisteredMenu(t, f.fake.URL)
	assert.Equal(t, "commands", first.Button.Type)
	require.Len(t, first.Commands, 2)
	assert.Equal(
		t,
		[]string{"passes", "massage", "orders"},
		[]string{first.Commands[""][0].Command, first.Commands[""][1].Command, first.Commands[""][2].Command},
	)
	assert.Equal(t, "Book a massage", first.Commands[""][1].Description)
	assert.Equal(t, "Запись на массаж", first.Commands["ru"][1].Description)
	restored, err := sandbox.New(t.Context(), f.db, "sandbox")
	require.NoError(t, err)
	restarted := httptest.NewServer(restored.Handler())
	t.Cleanup(restarted.Close)
	assert.Equal(t, first, readRegisteredMenu(t, restarted.URL))
	f.b.TG.Base = restarted.URL
	require.NoError(
		t,
		f.b.TG.SetMyCommands(t.Context(), "", []telegram.BotCommand{{Command: "old", Description: "Old deployment"}}),
	)
	runInboxUntil(t, f, func() bool { return readRegisteredMenu(t, restarted.URL).TotalCalls == 7 })
	after := readRegisteredMenu(t, restarted.URL)
	assert.Equal(t, first.Commands, after.Commands, "restart applies current catalog even when old menu exists")
}

func TestTelegramMenuPartialFailureReleasesLockAndRetriesAllSteps(t *testing.T) {
	t.Parallel()
	f := setup(t)
	post(t, f.fake.URL+"/lab/menu-fault", map[string]string{"method": "setMyCommands", "language_code": "ru"})
	require.ErrorContains(t, f.b.Run(t.Context()), "Telegram API error 429")
	partial := readRegisteredMenu(t, f.fake.URL)
	assert.Equal(t, 3, partial.TotalCalls)
	assert.NotContains(t, partial.Commands, "ru")
	require.Len(t, partial.Calls, 3)
	assert.False(t, partial.Calls[2].Applied)
	var cursorExists bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM bot.cursors WHERE name='telegram_received')`).
			Scan(&cursorExists),
	)
	assert.False(t, cursorExists, "polling does not start with a partial menu")
	runInboxUntil(t, f, func() bool { return readRegisteredMenu(t, f.fake.URL).TotalCalls == 6 })
	complete := readRegisteredMenu(t, f.fake.URL)
	require.Len(t, complete.Commands, 2)
	require.Len(t, complete.Calls, 6)
	assert.Equal(t, "setChatMenuButton", complete.Calls[3].Method)
	assert.True(t, complete.Calls[5].Applied)
}

func TestTelegramMenuStartupCancellationReleasesLock(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	blocked := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { cancel(); <-ctx.Done() }),
	)
	t.Cleanup(blocked.Close)
	f.b.TG.Base = blocked.URL
	require.NoError(t, f.b.Run(ctx), "normal shutdown retains Run cancellation semantics")
	f.b.TG.Base = f.fake.URL
	runInboxUntil(t, f, func() bool { return readRegisteredMenu(t, f.fake.URL).TotalCalls == 3 })
}
