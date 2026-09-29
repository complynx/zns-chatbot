package telegram_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestStructuredRetryAfter(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name       string
		parameters string
		seconds    int64
		malformed  bool
	}{
		{"cooldown", `{"retry_after":120}`, 120, false},
		{"absent", `{}`, 0, false},
		{"negative", `{"retry_after":-1}`, -1, false},
		{"large", `{"retry_after":9223372036854775807}`, 9223372036854775807, false},
		{"string", `{"retry_after":"120"}`, 0, true},
		{"overflow", `{"retry_after":9223372036854775808}`, 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(
					[]byte(
						`{"ok":false,"error_code":429,"description":"cooldown","parameters":` + scenario.parameters + `}`,
					),
				)
			}))
			t.Cleanup(server.Close)
			err := (telegram.Client{Base: server.URL, Token: "synthetic"}).Call(t.Context(), "sendMessage", nil, nil)
			require.Error(t, err)
			var apiErr *telegram.APIError
			if scenario.malformed {
				require.ErrorAs(t, err, &apiErr)
				assert.True(t, apiErr.Parameters.RetryAfterInvalid)
				return
			}
			require.ErrorAs(t, err, &apiErr)
			assert.Equal(t, http.StatusTooManyRequests, apiErr.Code)
			assert.Equal(t, "cooldown", apiErr.Description)
			assert.Equal(t, scenario.seconds, apiErr.Parameters.RetryAfter)
		})
	}
}
