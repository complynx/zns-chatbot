package bot_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestUpdateObserver(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	worker := bot.Bot{Observer: runtime, Logger: slog.New(slog.DiscardHandler)}
	require.NoError(t, worker.Handle(t.Context(), telegram.Update{}))
	metrics := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, metrics.Body.String(), `zns_operations_total{operation="telegram.update",result="ok"} 1`)
}
