package store_test

import (
	"context"
	"os"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/store"
)

type queryCounter struct {
	count       atomic.Int32
	connections atomic.Int32
}

func (q *queryCounter) TraceConnectStart(ctx context.Context, _ pgx.TraceConnectStartData) context.Context {
	q.connections.Add(1)
	return ctx
}
func (*queryCounter) TraceConnectEnd(context.Context, pgx.TraceConnectEndData) {}

func (q *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	q.count.Add(1)
	return ctx
}
func (*queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestOpenInstallsTracerBeforePing(t *testing.T) {
	t.Parallel()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for read-only pool verification")
	}
	var tracer queryCounter
	pool, err := store.Open(t.Context(), dsn, &tracer)
	require.NoError(t, err)
	defer pool.Close()
	assert.Positive(t, tracer.connections.Load())
	var value int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT 1").Scan(&value))
	assert.Equal(t, 1, value)
	assert.Positive(t, tracer.count.Load())
}

func TestOpenDoesNotEchoCredentials(t *testing.T) {
	t.Parallel()
	_, err := store.Open(t.Context(), "postgres://user:secret@%invalid")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
}
