package integration_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"
)

const admissionTestWait = 20 * time.Second

type admissionConnectionTrace struct{ cleanup chan chan struct{} }

func (tr admissionConnectionTrace) TraceQueryStart(
	ctx context.Context,
	conn *pgx.Conn,
	_ pgx.TraceQueryStartData,
) context.Context {
	select {
	case tr.cleanup <- conn.PgConn().CleanupDone():
	default:
	}
	return ctx
}

func (admissionConnectionTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func runtimeAdmissionConfig(db *pgxpool.Pool, label string) *pgx.ConnConfig {
	config := db.Config().ConnConfig.Copy()
	config.RuntimeParams["application_name"] = "synthetic_qa_admission_" + label
	return config
}

func acquireRuntimeAdmission(t *testing.T, config *pgx.ConnConfig, role runtimeapp.Role) *runtimeapp.Admission {
	t.Helper()
	admission, err := runtimeapp.Acquire(t.Context(), config, role)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), admissionTestWait)
		defer cancel()
		closeErr := admission.Close(ctx)
		require.NotErrorIs(t, closeErr, runtimeapp.ErrClose)
		if closeErr != nil {
			require.ErrorIs(t, closeErr, runtimeapp.ErrLost)
		}
	})
	return admission
}

func requireAdmissionSessions(t *testing.T, db *pgxpool.Pool, label string, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		var count int
		err := db.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity
 WHERE datname=current_database() AND application_name=$1`, "synthetic_qa_admission_"+label).Scan(&count)
		return err == nil && count == want
	}, admissionTestWait, 20*time.Millisecond)
}

func requireAdmissionSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(admissionTestWait):
		t.Fatal("admission lifecycle did not finish")
	}
}

func requireAdmissionCleanup(t *testing.T, trace admissionConnectionTrace) {
	t.Helper()
	select {
	case cleanup := <-trace.cleanup:
		requireAdmissionSignal(t, cleanup)
	case <-time.After(admissionTestWait):
		t.Fatal("admission connection was not observed")
	}
}

func TestRuntimeAdmissionRoleMatrix(t *testing.T) {
	t.Parallel()
	for _, owner := range []runtimeapp.Role{runtimeapp.API, runtimeapp.Bot, runtimeapp.App} {
		for _, contender := range []runtimeapp.Role{runtimeapp.API, runtimeapp.Bot, runtimeapp.App} {
			t.Run(fmt.Sprintf("%d-%d", owner, contender), func(t *testing.T) {
				t.Parallel()
				db := database(t)
				acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "owner"), owner)
				allowed := (owner == runtimeapp.API && contender == runtimeapp.Bot) ||
					(owner == runtimeapp.Bot && contender == runtimeapp.API)
				if allowed {
					acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "contender"), contender)
					return
				}
				admission, err := runtimeapp.Acquire(t.Context(), runtimeAdmissionConfig(db, "contender"), contender)
				require.ErrorIs(t, err, runtimeapp.ErrBusy)
				require.Nil(t, admission)
				requireAdmissionSessions(t, db, "contender", 0)
			})
		}
	}
}

func TestRuntimeAdmissionPartialAcquisitionAndCanceledStartup(t *testing.T) {
	t.Parallel()
	db := database(t)
	acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "bot"), runtimeapp.Bot)
	config := runtimeAdmissionConfig(db, "partial")
	trace := admissionConnectionTrace{cleanup: make(chan chan struct{}, 1)}
	config.Tracer = trace
	admission, err := runtimeapp.Acquire(t.Context(), config, runtimeapp.App)
	require.ErrorIs(t, err, runtimeapp.ErrBusy)
	require.Nil(t, admission)
	requireAdmissionCleanup(t, trace)
	requireAdmissionSessions(t, db, "partial", 0)
	acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "api"), runtimeapp.API)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	admission, err = runtimeapp.Acquire(canceled, runtimeAdmissionConfig(db, "canceled"), runtimeapp.API)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, admission)
	requireAdmissionSessions(t, db, "canceled", 0)
}

func TestRuntimeAdmissionParentCancelAndConcurrentClose(t *testing.T) {
	t.Parallel()
	db := database(t)
	parent, cancelParent := context.WithCancel(t.Context())
	defer cancelParent()
	config := runtimeAdmissionConfig(db, "closing")
	trace := admissionConnectionTrace{cleanup: make(chan chan struct{}, 1)}
	config.Tracer = trace
	admission, err := runtimeapp.Acquire(parent, config, runtimeapp.App)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(t.Context()), admissionTestWait)
		defer stop()
		require.NoError(t, admission.Close(cleanup))
	})
	cancelParent()
	config.RuntimeParams["application_name"] = "changed_after_acquisition"
	contender, err := runtimeapp.Acquire(t.Context(), runtimeAdmissionConfig(db, "busy"), runtimeapp.API)
	require.ErrorIs(t, err, runtimeapp.ErrBusy)
	require.Nil(t, contender)
	requireAdmissionSessions(t, db, "closing", 1)
	select {
	case <-admission.Done():
		t.Fatal("parent cancellation released admission")
	default:
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	err = admission.Close(canceled)
	require.ErrorIs(t, err, runtimeapp.ErrClose)
	require.ErrorIs(t, err, context.Canceled)
	var closers sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		closers.Go(func() {
			ctx, stop := context.WithTimeout(t.Context(), admissionTestWait)
			defer stop()
			errors <- admission.Close(ctx)
		})
	}
	closers.Wait()
	close(errors)
	for closeErr := range errors {
		require.NoError(t, closeErr)
	}
	requireAdmissionSignal(t, admission.Done())
	requireAdmissionCleanup(t, trace)
	require.NoError(t, admission.Err())
	require.NoError(t, admission.Close(canceled), "already joined close is idempotent")
	requireAdmissionSessions(t, db, "closing", 0)
	acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "replacement"), runtimeapp.App)
}

func terminateRuntimeAdmission(t *testing.T, db *pgxpool.Pool, label string) {
	t.Helper()
	var terminated bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT pg_terminate_backend(pid,1000)
 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1`,
		"synthetic_qa_admission_"+label).Scan(&terminated))
	require.True(t, terminated)
}

func TestRuntimeAdmissionLossIsIndependentOfBlockedWork(t *testing.T) {
	t.Parallel()
	db := database(t)
	admission := acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "lost"), runtimeapp.API)
	work, err := pgx.ConnectConfig(t.Context(), runtimeAdmissionConfig(db, "work"))
	require.NoError(t, err)
	defer func() { require.NoError(t, work.Close(context.WithoutCancel(t.Context()))) }()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	finished := make(chan error, 1)
	go func() { _, workErr := work.Exec(ctx, "SELECT pg_sleep(30)"); finished <- workErr }()
	require.Eventually(t, func() bool {
		var active bool
		queryErr := db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND application_name='synthetic_qa_admission_work' AND state='active')`).Scan(&active)
		return queryErr == nil && active
	}, time.Second*5, time.Millisecond*20)
	terminateRuntimeAdmission(t, db, "lost")
	select {
	case <-admission.Done():
	case <-time.After(4 * time.Second):
		t.Fatal("admission loss waited for unrelated work")
	}
	require.ErrorIs(t, admission.Err(), runtimeapp.ErrLost)
	select {
	case <-finished:
		t.Fatal("work did not remain independently blocked")
	default:
	}
	cancel()
	require.Error(t, <-finished)
	requireAdmissionSignal(t, work.PgConn().CleanupDone())
	acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "replacement"), runtimeapp.API)
	require.ErrorIs(t, admission.Close(t.Context()), runtimeapp.ErrLost)
	requireAdmissionSessions(t, db, "lost", 0)
}

// A session lock is not a transaction fence: a separate old transaction can
// commit after a replacement obtains admission. Preserve this explicit limit.
func TestRuntimeAdmissionDoesNotFenceOldTransaction(t *testing.T) {
	t.Parallel()
	db := database(t)
	_, err := db.Exec(t.Context(), "CREATE TABLE admission_limit_proof (value integer)")
	require.NoError(t, err)
	old := acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "old"), runtimeapp.API)
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) }()
	_, err = tx.Exec(t.Context(), "INSERT INTO admission_limit_proof VALUES(1)")
	require.NoError(t, err)
	terminateRuntimeAdmission(t, db, "old")
	requireAdmissionSignal(t, old.Done())
	require.ErrorIs(t, old.Err(), runtimeapp.ErrLost)
	acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "new"), runtimeapp.API)
	require.NoError(t, tx.Commit(t.Context()), "old work can still commit after new admission")
	var value int
	require.NoError(t, db.QueryRow(t.Context(), "SELECT value FROM admission_limit_proof").Scan(&value))
	require.Equal(t, 1, value)
	require.ErrorIs(t, old.Close(t.Context()), runtimeapp.ErrLost)
}

func TestRuntimeAdmissionConnectionErrorsAreSanitized(t *testing.T) {
	t.Parallel()
	config := runtimeAdmissionConfig(database(t), "failed")
	config.Database = "private_database_marker"
	config.Password = "private_password_marker"
	admission, err := runtimeapp.Acquire(t.Context(), config, runtimeapp.API)
	require.Nil(t, admission)
	require.ErrorIs(t, err, runtimeapp.ErrUnavailable)
	require.EqualError(t, err, "runtime admission unavailable")
	require.NotContains(t, err.Error(), config.Password)
	require.NotContains(t, err.Error(), config.Database)
}

// This local TCP relay drops probe responses after successful acquisition.
// It owns and joins every accepted connection, including pgx cancellation.
func runtimeAdmissionProxy(t *testing.T, target string) (string, *atomic.Bool) {
	t.Helper()
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	drop := &atomic.Bool{}
	var workers sync.WaitGroup
	var mu sync.Mutex
	clients := map[net.Conn]bool{}
	workers.Go(func() {
		for {
			client, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			clients[client] = true
			mu.Unlock()
			workers.Go(func() {
				defer func() {
					_ = client.Close()
					mu.Lock()
					delete(clients, client)
					mu.Unlock()
				}()
				server, dialErr := (&net.Dialer{}).DialContext(t.Context(), "tcp", target)
				if dialErr != nil {
					return
				}
				defer server.Close()
				copied := make(chan struct{})
				go func() {
					_, _ = io.Copy(server, client)
					_ = server.Close()
					close(copied)
				}()
				forwardAdmissionResponses(client, server, drop)
				_ = client.Close()
				<-copied
			})
		}
	})
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		for client := range clients {
			_ = client.Close()
		}
		mu.Unlock()
		workers.Wait()
	})
	return listener.Addr().String(), drop
}

func forwardAdmissionResponses(client, server net.Conn, drop *atomic.Bool) {
	buffer := make([]byte, 4096)
	for {
		n, err := server.Read(buffer)
		if n > 0 && !drop.Load() {
			if _, err = client.Write(buffer[:n]); err != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func TestRuntimeAdmissionProbeDeadlineAndCleanup(t *testing.T) {
	t.Parallel()
	db := database(t)
	config := runtimeAdmissionConfig(db, "timeout")
	address, drop := runtimeAdmissionProxy(t, net.JoinHostPort(config.Host, strconv.Itoa(int(config.Port))))
	host, port, err := net.SplitHostPort(address)
	require.NoError(t, err)
	portNumber, err := strconv.ParseUint(port, 10, 16)
	require.NoError(t, err)
	config.Host, config.Port, config.TLSConfig, config.Fallbacks = host, uint16(portNumber), nil, nil
	trace := admissionConnectionTrace{cleanup: make(chan chan struct{}, 1)}
	config.Tracer = trace
	admission := acquireRuntimeAdmission(t, config, runtimeapp.API)
	drop.Store(true)
	select {
	case <-admission.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("probe did not respect its deadline")
	}
	require.ErrorIs(t, admission.Err(), runtimeapp.ErrLost)
	cleanup, cancel := context.WithTimeout(t.Context(), admissionTestWait)
	defer cancel()
	require.ErrorIs(t, admission.Close(cleanup), runtimeapp.ErrLost)
	requireAdmissionCleanup(t, trace)
	requireAdmissionSessions(t, db, "timeout", 0)
	acquireRuntimeAdmission(t, runtimeAdmissionConfig(db, "after_timeout"), runtimeapp.API)
}
