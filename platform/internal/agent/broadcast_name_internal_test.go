package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastNameSourceNormalization(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ text, want string }{
		{"Анализ: legal_name даёт Иван.\n\n  Ваня  ", "Ваня"},
		{"Анализ: известное имя.\n\nАнна Мария", "Анна Мария"},
		{"Ваня", ""}, {"Анализ\nВаня\n", ""}, {"", ""},
		{"Анализ\nИмя не указано", ""}, {"Анализ\nИмя не указанно", ""}, {"Анализ\nимя не указано", ""},
	} {
		assert.Equal(t, test.want, normalizeBroadcastName(test.text), test.text)
	}
}

func TestBroadcastNameProjectsNamesAndRedactsFailures(t *testing.T) {
	t.Parallel()
	fields := map[string]any{"legal_name": "Иван Иванов", "inner_name_by": "Ян", "known_names": []string{"Иван"},
		"preferred_name": "Ваня", "user_name": "generated-canary", "user_informal_name": "derived-canary",
		"passport_number": "passport-canary", "bot_username": "bot-canary", "email": "email-canary"}
	name, err := generateBroadcastName(
		t.Context(),
		fields,
		func(_ context.Context, prompt providerPrompt) (string, error) {
			assert.Contains(t, prompt.instructions, "UNTRUSTED")
			assert.Contains(t, prompt.instructions, "legal_name, then known_names")
			assert.Contains(t, string(prompt.input), "inner_name_by")
			assert.Contains(t, string(prompt.input), "preferred_name")
			assert.NotContains(t, string(prompt.input), "canary")
			return `{"text":"Анализ: надёжное имя.\n\nВаня"}`, nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "Ваня", name)
	_, err = generateBroadcastName(t.Context(), fields, func(context.Context, providerPrompt) (string, error) {
		return "", errors.New("provider-private-canary")
	})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "canary")
	for _, raw := range []string{`{"text":"a","text":"b"}`, `{"text":null}`, `{"text":"x","other":1}`, strings.Repeat("x", maxBroadcastNameBytes+1)} {
		_, err = decodeBroadcastName([]byte(raw))
		require.Error(t, err)
	}
}

func TestBroadcastNameOpenAIAndRemoteUseExistingProviderBoundary(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/responses", r.URL.Path)
		assert.Equal(t, "Bearer synthetic-key", r.Header.Get("Authorization"))
		var input map[string]json.RawMessage
		assert.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		assert.NotContains(t, string(input["input"]), "passport-canary")
		assert.Contains(t, string(input["text"]), "zns_broadcast_name")
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{"status": "completed", "output": []any{
			map[string]any{
				"type": "message",
				"content": []any{
					map[string]string{"type": "output_text", "text": `{"text":"Анализ: имя из legal_name.\n\nДаша"}`},
				},
			},
		}}))
	}))
	t.Cleanup(upstream.Close)
	model := OpenAI{Key: "synthetic-key", BaseURL: upstream.URL}
	server := httptest.NewServer(ModelHandler(model))
	t.Cleanup(server.Close)
	name, err := (Remote{URL: server.URL}).InformalName(
		t.Context(),
		map[string]any{"legal_name": "Дарья", "passport_number": "passport-canary"},
	)
	require.NoError(t, err)
	assert.Equal(t, "Даша", name)
	_, err = (Codex{Executable: "relative", SyntheticOnly: true}).InformalName(t.Context(), map[string]any{})
	require.Error(t, err)
}

func TestBroadcastNameCancellationAndBounds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := generateBroadcastName(ctx, map[string]any{}, func(ctx context.Context, _ providerPrompt) (string, error) {
		return "", ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	_, err = broadcastNameInput(map[string]any{"legal_name": strings.Repeat("x", maxBroadcastNameBytes)})
	require.Error(t, err)
}
