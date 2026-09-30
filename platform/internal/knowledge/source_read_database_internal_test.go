package knowledge

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type r38ActorQuery struct{}

type r38ReadFault struct {
	prefix    string
	cancel    context.CancelFunc
	actorRead bool
	fired     bool
	closeErr  error
}

func (f *r38ReadFault) TraceQueryStart(
	ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData,
) context.Context {
	if strings.HasPrefix(data.SQL, f.prefix) && !f.fired {
		f.fired = true
		if f.cancel != nil {
			f.cancel()
		} else {
			f.closeErr = conn.Close(ctx)
		}
	}
	return context.WithValue(
		ctx,
		r38ActorQuery{},
		strings.HasPrefix(data.SQL, "SELECT EXISTS(SELECT 1 FROM core.users"),
	)
}

func (f *r38ReadFault) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if actor, ok := ctx.Value(r38ActorQuery{}).(bool); ok && actor && data.Err == nil {
		f.actorRead = true
	}
}

func TestSourceReadDatabaseFailureAfterKnownActor(t *testing.T) {
	t.Parallel()
	db := r30Database(t)
	service := Service{DB: db}
	identity := strings.Repeat("a", 64)
	require.NoError(t, service.ConfigureSource(t.Context(), AssistantQA, identity))
	err := service.ReplaceSource(t.Context(), AssistantQA, identity, strings.Repeat("b", 64), []string{"source body"})
	require.NoError(t, err)
	ref := memoryReference{
		Namespace:  MemoryShared,
		Topic:      AssistantQA,
		Key:        identity + ".00000.00000",
		Version:    1,
		SourceKind: MemorySourceKind,
	}
	for _, mode := range []string{"transport", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fault := &r38ReadFault{prefix: "SELECT d.body,v.source_digest"}
			if mode == "cancel" {
				fault.cancel = cancel
			}
			config := db.Config()
			config.ConnConfig.Tracer = fault
			broken, queryErr := pgxpool.NewWithConfig(ctx, config)
			require.NoError(t, queryErr)
			t.Cleanup(broken.Close)
			_, queryErr = (Service{DB: broken}).readSource(ctx, "alice", ref, MemoryEntry{})
			require.True(t, fault.actorRead, "knownActor must complete before the source query fails")
			require.True(t, fault.fired)
			require.NoError(t, fault.closeErr)
			if mode == "cancel" {
				require.ErrorIs(t, queryErr, context.Canceled)
				require.False(t, core.IsDatabaseFailure(queryErr))
			} else {
				require.ErrorIs(t, queryErr, core.ErrDatabase)
				require.EqualError(t, queryErr, core.ErrDatabase.Error())
			}
		})
	}
	entry, err := service.readSource(t.Context(), "alice", ref, MemoryEntry{})
	require.NoError(t, err)
	require.Equal(t, "source body", entry.Text)
	require.True(t, entry.Active)
	missing := ref
	missing.Version++
	_, err = service.readSource(t.Context(), "alice", missing, MemoryEntry{})
	var stale *core.ProblemError
	require.ErrorAs(t, err, &stale)
	require.Equal(t, "knowledge_stale", stale.Code)
	require.False(t, core.IsDatabaseFailure(err))
	_, err = service.readSource(t.Context(), "missing-actor", ref, MemoryEntry{})
	require.ErrorAs(t, err, &stale)
	require.Equal(t, "forbidden", stale.Code)
}

// This codec fails only while scanning the SQL bigint version column, after Query succeeds.
type r38RowFaultCodec struct {
	pgtype.Int8Codec

	scanned *bool
}

func (codec r38RowFaultCodec) PlanScan(*pgtype.Map, uint32, int16, any) pgtype.ScanPlan {
	return r38RowFaultPlan{scanned: codec.scanned}
}

type r38RowFaultPlan struct{ scanned *bool }

func (plan r38RowFaultPlan) Scan([]byte, any) error {
	*plan.scanned = true
	return io.EOF
}

func TestSourceStatusesDatabaseQueryAndRowFailures(t *testing.T) {
	t.Parallel()
	db := r30Database(t)
	service := Service{DB: db}
	statuses, err := service.SourceStatuses(t.Context())
	require.NoError(t, err)
	require.Empty(t, statuses)
	require.NoError(t, service.ConfigureSource(t.Context(), AssistantAbout, strings.Repeat("a", 64)))
	for _, stage := range []string{"query", "row"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			config := db.Config()
			fault := &r38ReadFault{prefix: "SELECT slot,status,version"}
			rowScanned := false
			if stage == "query" {
				config.ConnConfig.Tracer = fault
			} else {
				config.AfterConnect = func(_ context.Context, conn *pgx.Conn) error {
					conn.TypeMap().RegisterType(&pgtype.Type{
						Name: "int8", OID: pgtype.Int8OID, Codec: r38RowFaultCodec{scanned: &rowScanned},
					})
					return nil
				}
			}
			broken, openErr := pgxpool.NewWithConfig(t.Context(), config)
			require.NoError(t, openErr)
			t.Cleanup(broken.Close)
			_, readErr := (Service{DB: broken}).SourceStatuses(t.Context())
			require.ErrorIs(t, readErr, core.ErrDatabase)
			require.EqualError(t, readErr, core.ErrDatabase.Error())
			if stage == "query" {
				require.True(t, fault.fired)
				require.NoError(t, fault.closeErr)
			} else {
				require.True(t, rowScanned)
			}
		})
	}
	statuses, err = service.SourceStatuses(t.Context())
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	require.Equal(t, AssistantAbout, statuses[0].Slot)
}
