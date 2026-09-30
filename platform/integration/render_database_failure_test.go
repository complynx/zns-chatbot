package integration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type renderReadFailure struct {
	query    string
	fired    bool
	closeErr error
}

func (tr *renderReadFailure) TraceQueryStart(
	ctx context.Context,
	conn *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	if !tr.fired && strings.HasPrefix(data.SQL, tr.query) {
		tr.fired = true
		tr.closeErr = conn.Close(ctx)
	}
	return ctx
}

func (*renderReadFailure) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestRenderDatabaseFailureAfterHealthyDomainReads(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"profile", "profile buttons", "profile cleanup", "orders", "knowledge"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			fault := &renderReadFailure{query: "SELECT content,kind,native_markdown,update_id"}
			render := f.b.RenderProfile
			if kind == "profile buttons" {
				fault.query = "INSERT INTO bot.profile_buttons"
			}
			if kind == "profile cleanup" {
				fault.query = "DELETE FROM bot.profile_buttons"
			}
			if kind == "orders" {
				fault.query = "SELECT r.content,r.native_markdown,r.update_id"
				render = f.b.RenderOrders
			}
			if kind == "knowledge" {
				fault.query = "SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=0"
				render = f.b.RenderKnowledge
			}
			poolConfig := f.db.Config()
			poolConfig.ConnConfig.Tracer = fault
			pool, err := pgxpool.NewWithConfig(t.Context(), poolConfig)
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			f.b.DB = pool
			err = render(t.Context(), "alice", 101)
			require.True(t, fault.fired, "healthy domain reads must reach the later rendering SQL")
			require.NoError(t, fault.closeErr)
			require.Equal(t, core.ErrDatabase, err)
			require.NoError(t, f.db.Ping(t.Context()), "only the rendering SQL connection should fail")
		})
	}
}
