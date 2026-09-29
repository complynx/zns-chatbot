package integration_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func mutationFixture(t *testing.T) (*pgxpool.Pool, derivedmutation.Service, readsource.Derivation) {
	t.Helper()
	db := database(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.knowledge_permissions(scope,actor,permission) VALUES('','alice','review')`,
	)
	require.NoError(t, err)
	generation := int64(0)
	return db, derivedmutation.Service{
		DB:       db,
		Orders:   orders.Service{DB: db},
		Workflow: workflow.Service{DB: db},
	}, readsource.Derivation{
		Generation: &generation,
		Authorities: []readsource.Authority{
			{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review}},
		},
	}
}

func derivedCreate(source readsource.Derivation, key string) orders.Command {
	return orders.Command{EventID: "sandbox-festival", Name: "create", Origin: "agent", Key: key,
		HistoryGeneration: source.Generation, Choice: &orders.ChoiceInput{Customer: "derived source"}}
}

func TestDerivedMutationSourceRevocationAndAuthorizedReplay(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "workflow"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			db, service, source := mutationFixture(t)
			call := func(key string) error {
				if domain == "orders" {
					_, err := service.ExecuteOrder(t.Context(), "alice", derivedCreate(source, key), source)
					return err
				}
				_, err := service.ExecuteWorkflow(
					t.Context(),
					"alice",
					action("select", "massage-1", 0, key, "agent"),
					source,
				)
				return err
			}
			require.NoError(t, call("committed"))
			_, err := db.Exec(t.Context(), `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
			require.NoError(t, err)
			require.NoError(t, call("committed"), "receipt survives source loss")
			requireCode(t, call("new-effect"), "source_stale")
			_, err = db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
			require.NoError(t, err)
			requireCode(t, call("committed"), "forbidden")
		})
	}
}

func TestDerivedMutationHistoryAndMissingEvidence(t *testing.T) {
	t.Parallel()
	db, service, source := mutationFixture(t)
	history := conversation.Service{DB: db}
	require.NoError(t, history.AppendOriginal(t.Context(), "alice", "mutation-source", "user", "private source"))
	var id int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE source_key='mutation-source'`).
			Scan(&id),
	)
	command := derivedCreate(source, "before-delete")
	created, err := service.ExecuteOrder(t.Context(), "alice", command, source)
	require.NoError(t, err)
	require.NoError(t, history.DeleteContent(t.Context(), "alice", id))
	replay, err := service.ExecuteOrder(t.Context(), "alice", command, source)
	require.NoError(t, err)
	require.Equal(t, created.ID, replay.ID)
	command.Key = "after-delete"
	_, err = service.ExecuteOrder(t.Context(), "alice", command, source)
	requireCode(t, err, "history_stale")
	_, err = service.ExecuteWorkflow(
		t.Context(),
		"alice",
		action("select", "massage-1", 0, "after-delete", "agent"),
		source,
	)
	requireCode(t, err, "history_stale")
	for _, missing := range []readsource.Derivation{{Authorities: []readsource.Authority{}}, {Generation: source.Generation}} {
		_, err = service.ExecuteOrder(t.Context(), "alice", command, missing)
		requireCode(t, err, "invalid_derivation")
	}
	manual := orders.Command{
		EventID: command.EventID,
		Name:    "create",
		Origin:  "manual",
		Key:     "manual",
		Choice:  &orders.ChoiceInput{},
	}
	_, err = service.Orders.Execute(t.Context(), "alice", manual)
	require.NoError(t, err, "manual new effects are not derived from deleted history")
}

func waitMutationBlocked(t *testing.T, db *pgxpool.Pool, blocker int32, count int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var waiting int
		err := db.QueryRow(t.Context(), `WITH RECURSIVE waiters(pid) AS (
 SELECT $1::integer UNION
 SELECT a.pid FROM pg_stat_activity a JOIN waiters w ON w.pid=ANY(pg_blocking_pids(a.pid))
 WHERE a.datname=current_database() AND a.wait_event_type='Lock'
 ) SELECT count(*)-1 FROM waiters`, blocker).Scan(&waiting)
		return err == nil && waiting >= count
	}, 5*time.Second, 10*time.Millisecond, "expected PostgreSQL lock barrier was not reached")
}

func mutationEventBarrier(t *testing.T, db *pgxpool.Pool) (pgx.Tx, int32) {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	var pid int32
	require.NoError(t, tx.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid))
	_, err = tx.Exec(t.Context(), `SELECT id FROM core.order_events WHERE id='sandbox-festival' FOR UPDATE`)
	require.NoError(t, err)
	return tx, pid
}

func TestDerivedOrderRevocationWinsAtTargetCommitBarrier(t *testing.T) {
	t.Parallel()
	db, service, source := mutationFixture(t)
	barrier, pid := mutationEventBarrier(t, db)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := service.ExecuteOrder(ctx, "alice", derivedCreate(source, "blocked-source"), source)
		done <- err
	}()
	waitMutationBlocked(t, db, pid, 1)
	_, err := db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
	require.NoError(t, err)
	require.NoError(t, barrier.Commit(ctx))
	requireCode(t, <-done, "source_stale")
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.orders`).Scan(&count))
	require.Zero(t, count)
}

func TestManualAndDerivedOrdersAcquireEventBeforeActor(t *testing.T) {
	t.Parallel()
	db, service, source := mutationFixture(t)
	barrier, pid := mutationEventBarrier(t, db)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 2)
	go func() {
		command := derivedCreate(source, "manual-competing")
		command.Origin, command.HistoryGeneration = "manual", nil
		_, err := service.Orders.Execute(ctx, "alice", command)
		done <- err
	}()
	waitMutationBlocked(t, db, pid, 1)
	go func() {
		_, err := service.ExecuteOrder(ctx, "alice", derivedCreate(source, "derived-competing"), source)
		done <- err
	}()
	waitMutationBlocked(t, db, pid, 2)
	probe, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = probe.Rollback(context.WithoutCancel(t.Context())) })
	_, err = probe.Exec(ctx, `SELECT id FROM core.users WHERE id='alice' FOR NO KEY UPDATE NOWAIT`)
	require.NoError(t, err, "neither blocked executor may hold actor while awaiting target event")
	require.NoError(t, probe.Rollback(ctx))
	require.NoError(t, barrier.Commit(ctx))
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	var count int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.orders`).Scan(&count))
	require.Equal(t, 2, count)
}

type mutationCommitBarrier struct {
	entered chan uint32
	release chan struct{}
	once    sync.Once
}

type mutationReceiptKey struct{}

func (b *mutationCommitBarrier) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	data pgx.TraceQueryStartData,
) context.Context {
	return context.WithValue(ctx, mutationReceiptKey{}, strings.Contains(data.SQL, "INSERT INTO core.order_operations"))
}

func (b *mutationCommitBarrier) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, _ pgx.TraceQueryEndData) {
	if selected, _ := ctx.Value(mutationReceiptKey{}).(bool); !selected {
		return
	}
	b.once.Do(func() {
		b.entered <- conn.PgConn().PID()
		select {
		case <-b.release:
		case <-ctx.Done():
		}
	})
}

func TestDerivedOrderRetainsSourceGrantUntilCommit(t *testing.T) {
	t.Parallel()
	db, service, source := mutationFixture(t)
	barrier := &mutationCommitBarrier{entered: make(chan uint32, 1), release: make(chan struct{})}
	config := db.Config()
	config.ConnConfig.Tracer = barrier
	traced, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(traced.Close)
	service.DB = traced
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, runErr := service.ExecuteOrder(ctx, "alice", derivedCreate(source, "grant-held"), source)
		done <- runErr
	}()
	var pid uint32
	select {
	case pid = <-barrier.entered:
	case <-ctx.Done():
		t.Fatal("receipt insertion barrier not reached", ctx.Err())
	}
	revoked := make(chan error, 1)
	go func() {
		_, deleteErr := db.Exec(ctx, `DELETE FROM core.knowledge_permissions WHERE actor='alice'`)
		revoked <- deleteErr
	}()
	waitMutationBlocked(t, db, int32(pid), 1)
	close(barrier.release)
	require.NoError(t, <-done)
	require.NoError(t, <-revoked)
	_, err = service.ExecuteOrder(ctx, "alice", derivedCreate(source, "grant-held"), source)
	require.NoError(t, err, "committed receipt remains truthful after delayed revocation")
}
