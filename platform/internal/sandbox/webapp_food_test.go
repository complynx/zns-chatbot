package sandbox_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestFoodWebAppLaunchUsesOwnedButton(t *testing.T) {
	t.Parallel()
	fake := &sandbox.Fake{Token: "sandbox"}
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "sandbox"}
	address := "https://example.test/menu?order_id=legacy&pass_key=event"
	message, err := client.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Menu", Markup: telegram.Markup{
		Rows: [][]telegram.Button{{{Text: "Menu", WebApp: &telegram.WebApp{URL: address}}}},
	}})
	require.NoError(t, err)
	for _, user := range []int64{101, 202} {
		payload, marshalErr := json.Marshal(map[string]any{"user": user, "message_id": message.ID, "url": address})
		require.NoError(t, marshalErr)
		request := httptest.NewRequest(http.MethodPost, "/lab/webapp", bytes.NewReader(payload))
		request.Header.Set("X-Sandbox", "1")
		response := httptest.NewRecorder()
		fake.Handler().ServeHTTP(response, request)
		if user != 101 {
			require.Equal(t, http.StatusForbidden, response.Code)
			continue
		}
		require.Equal(t, http.StatusOK, response.Code)
		var result struct {
			URL string `json:"url"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		launched, parseErr := url.Parse(result.URL)
		require.NoError(t, parseErr)
		require.Equal(t, "/menu", launched.Path)
		require.Equal(t, "legacy", launched.Query().Get("order_id"))
		require.Equal(t, "event", launched.Query().Get("pass_key"))
		fragment, fragmentErr := url.ParseQuery(launched.EscapedFragment())
		require.NoError(t, fragmentErr)
		identity, verifyErr := telegram.VerifyWebApp(fragment.Get("tgWebAppData"), fake.Token, time.Now())
		require.NoError(t, verifyErr)
		require.Equal(t, int64(101), identity.ID)
	}
}
