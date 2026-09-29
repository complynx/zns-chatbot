package sandbox_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestLargeBacklogIsBoundedAndDrains(t *testing.T) {
	t.Parallel()
	fake := &sandbox.Fake{Token: "sandbox"}
	server := httptest.NewServer(fake.Handler())
	t.Cleanup(server.Close)
	payload, err := json.Marshal(map[string]any{"user": 101, "text": strings.Repeat("<", 5000)})
	require.NoError(t, err)
	for range 600 {
		request, requestError := http.NewRequestWithContext(
			t.Context(),
			http.MethodPost,
			server.URL+"/lab/input",
			bytes.NewReader(payload),
		)
		require.NoError(t, requestError)
		request.Header.Set("X-Sandbox", "1")
		response, responseError := server.Client().Do(request)
		require.NoError(t, responseError)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusOK, response.StatusCode)
	}
	client := telegram.Client{Base: server.URL, Token: "sandbox"}
	offset := int64(0)
	total := 0
	for total < 600 {
		updates, updateError := client.Updates(t.Context(), offset)
		require.NoError(t, updateError)
		require.NotEmpty(t, updates)
		require.LessOrEqual(t, len(updates), 100)
		total += len(updates)
		offset = updates[len(updates)-1].ID + 1
	}
	updates, err := client.Updates(t.Context(), offset)
	require.NoError(t, err)
	require.Empty(t, updates)
}

func TestRequiresExplicitSandbox(t *testing.T) {
	t.Parallel()
	for _, env := range []string{"", "production", "dev"} {
		require.Error(t, sandbox.RequireSandbox(env), "accepted %s", env)
	}
	require.NoError(t, sandbox.RequireSandbox("sandbox"))
}

func TestLanguageFixtureReachesTelegramUpdate(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"ua", "be-BY", "pl", strings.Repeat("x", 65)} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer((&sandbox.Fake{Token: "sandbox"}).Handler())
			t.Cleanup(server.Close)
			body, err := json.Marshal(map[string]any{"user": 101, "text": "/language", "language_code": language})
			require.NoError(t, err)
			request, err := http.NewRequestWithContext(
				t.Context(),
				http.MethodPost,
				server.URL+"/lab/input",
				bytes.NewReader(body),
			)
			require.NoError(t, err)
			request.Header.Set("X-Sandbox", "1")
			response, err := server.Client().Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			if len(language) > 64 {
				require.Equal(t, http.StatusBadRequest, response.StatusCode)
				return
			}
			require.Equal(t, http.StatusOK, response.StatusCode)
			var update telegram.Update
			require.NoError(t, json.NewDecoder(response.Body).Decode(&update))
			require.Equal(t, language, update.Message.From.LanguageCode)
			client := telegram.Client{Base: server.URL, Token: "sandbox"}
			updates, err := client.Updates(t.Context(), 0)
			require.NoError(t, err)
			require.Len(t, updates, 1)
			require.Equal(t, language, updates[0].Message.From.LanguageCode)
		})
	}
}
