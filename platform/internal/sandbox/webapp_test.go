package sandbox_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestMiniAppProxyCoexistsWithBotAPI(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream-Path", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(upstream.Close)
	fake, err := sandbox.New(context.Background(), nil, "sandbox")
	require.NoError(t, err)
	fake.MiniAppURL = upstream.URL
	handler := fake.Handler()
	paths := []string{
		"/miniapp/",
		"/miniapp/editor.js",
		"/miniapp/editor.css",
		"/miniapp/api/orders/test",
		"/miniapp/massage",
		"/massage_timetable",
		"/miniapp/timetable.js",
		"/miniapp/timetable.css",
		"/miniapp/api/massage/timetable",
		"/miniapp/browser-auth.js",
		"/menu",
		"/miniapp/legacyfood.js",
		"/miniapp/legacyfood.css",
		"/miniapp/foodphotos/meal.jpg",
		"/miniapp/api/food",
	}
	for _, path := range []string{"/menu", "/miniapp/api/food", "/miniapp/api/food/quote"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		assert.Equal(t, http.StatusNoContent, response.Code)
		assert.Equal(t, path, response.Header().Get("X-Upstream-Path"))
	}
	for _, path := range paths {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusNoContent, response.Code)
		assert.Equal(t, path, response.Header().Get("X-Upstream-Path"))
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/botsandbox/getMe", nil))
	assert.Equal(t, http.StatusOK, response.Code)
}
