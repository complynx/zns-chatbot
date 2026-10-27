package task

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestMenuChangesOnlyAfterAvailabilityChangesAndClearsAccess(t *testing.T) {
	t.Parallel()
	var calls []map[string]any
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			return
		}
		calls = append(calls, body)
		if fail {
			fmt.Fprint(w, `{"ok":false,"error_code":500,"description":"Unavailable"}`)
			return
		}
		fmt.Fprint(w, `{"ok":true,"result":true}`)
	}))
	defer server.Close()
	client, err := telegram.New(server.URL, "123:secret", server.Client())
	require.NoError(t, err)
	s := &Service{tg: client}
	apply := func(a db.TelegramMenuAvailability) error {
		return s.applyMenu(context.Background(), 42, startCommands(false, a, true), startCommands(true, a, true))
	}
	require.NoError(t, apply(db.TelegramMenuAvailability{}))
	require.Len(t, calls, 3)
	require.NoError(t, apply(db.TelegramMenuAvailability{}))
	require.Len(t, calls, 3, "unchanged snapshot makes no Telegram calls")
	fail = true
	require.Error(t, apply(db.TelegramMenuAvailability{Register: true}))
	fail = false
	require.NoError(t, apply(db.TelegramMenuAvailability{Register: true}))
	require.Len(t, calls, 7, "failed application must retry")
	require.NoError(t, s.applyMenu(context.Background(), 42, nil, nil))
	require.Equal(t, []any{}, calls[7]["commands"])
	require.Equal(t, []any{}, calls[8]["commands"])
	require.NoError(t, s.applyMenu(context.Background(), 42, nil, nil))
	require.Len(t, calls, 10)
}
