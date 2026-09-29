package observability_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

type plainWriter struct {
	header http.Header
	status int
}

func (w *plainWriter) Header() http.Header            { return w.header }
func (w *plainWriter) Write(body []byte) (int, error) { return len(body), nil }
func (w *plainWriter) WriteHeader(status int)         { w.status = status }

func TestHTTPWriterInterfacesAndPanic(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	plain := &plainWriter{header: make(http.Header)}
	handler := runtime.HTTPHandler("server", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, flushes := w.(http.Flusher)
		assert.False(t, flushes)
		w.Header().Set("X-Test", "preserved")
		w.WriteHeader(http.StatusAccepted)
	}))
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusAccepted, plain.status)
	assert.Equal(t, "preserved", plain.Header().Get("X-Test"))
	recorder := httptest.NewRecorder()
	runtime.HTTPHandler("server", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if assert.True(t, ok) {
			flusher.Flush()
		}
		_, writeErr := io.WriteString(w, "stream")
		assert.NoError(t, writeErr)
	})).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.True(t, recorder.Flushed)
	assert.Equal(t, "stream", recorder.Body.String())
	assert.PanicsWithValue(t, "expected", func() {
		runtime.HTTPHandler("server", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("expected")
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
	metrics := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Contains(t, metrics.Body.String(), `zns_operations_total{operation="server",result="error"} 1`)
}

func TestPoolAndQueryMetricsExcludeSQL(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	pool, err := pgxpool.New(t.Context(), "postgres://user:private@127.0.0.1:1/db")
	require.NoError(t, err)
	defer pool.Close()
	require.NoError(t, runtime.RegisterPool(pool))
	tracer := runtime.PGXTracer()
	ctx := tracer.TraceQueryStart(
		t.Context(),
		nil,
		pgx.TraceQueryStartData{SQL: "SELECT 'secret'", Args: []any{"private"}},
	)
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	metrics := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Contains(t, metrics.Body.String(), "zns_db_pool_connections")
	assert.Contains(t, metrics.Body.String(), `zns_operations_total{operation="db",result="ok"} 1`)
	assert.NotContains(t, metrics.Body.String(), "secret")
	assert.NotContains(t, metrics.Body.String(), "private")
}
