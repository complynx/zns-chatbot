package integration_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type memoryReadStateFixture struct {
	fixture     *fixture
	host        appclient.Host
	application *pgxpool.Pool
	revoked     atomic.Bool
}

func newMemoryReadStateFixture(t *testing.T, local bool) *memoryReadStateFixture {
	t.Helper()
	f := memorySplitRoleFixture(t)
	// Reproduce the existing sandbox application owner in this isolated database.
	_, err := f.db.Exec(t.Context(),
		`GRANT USAGE ON SCHEMA core TO zns_api;
 GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA core TO zns_api;
 GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA core TO zns_api`)
	require.NoError(t, err)
	config := f.db.Config()
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, roleErr := conn.Exec(ctx, "SET ROLE zns_api")
		return roleErr
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	result := &memoryReadStateFixture{fixture: f, application: pool}
	signer := identity.Signer{Key: []byte(strings.Repeat("k", identity.MinKeyBytes))}
	verify := func(_ context.Context, token string) (string, error) {
		if result.revoked.Load() {
			return "", identity.ErrZitadelUserInactive
		}
		return signer.Verify(token)
	}
	services := appservices.NewServices(pool, appservices.Options{})
	server := httptest.NewServer(api.AuthenticatedHandler(services, signer, slog.New(slog.DiscardHandler), verify))
	t.Cleanup(server.Close)
	result.host = f.b.Host
	result.host.Base = server.URL
	if local {
		result.host.LocalMemoryReadState = &appclient.LocalMemoryReadState{
			Service:    services.MemoryReadState,
			Authorizer: applicationauth.Authorizer{DB: pool, Verify: verify},
		}
	}
	return result
}

func TestMemoryReadStateApplicationOwner(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"local", "http"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newMemoryReadStateFixture(t, mode == "local")
			ctx := t.Context()
			service := knowledge.Service{DB: f.application}
			_, err := service.Execute(ctx, "alice", knowledge.Command{
				Name: knowledge.MemoSet, Key: "memory-state-create", FactKey: "train", Text: "private train note",
			})
			require.NoError(t, err)
			state, err := service.MemoryDeletions(ctx, "alice")
			require.NoError(t, err)
			request := agent.KnowledgeProposal{Name: agent.KnowledgeMemoRead, FactKey: "train"}
			index, err := f.host.ReserveKnowledge(ctx, "alice", 17911, request)
			require.NoError(t, err)
			require.Zero(t, index)
			store := agenthost.ReadStore{DB: f.fixture.b.DB, Memory: f.host}
			pending, err := store.Knowledge(ctx, "alice", 17911)
			require.NoError(t, err)
			require.Len(t, pending, 1)
			require.Equal(t, "interrupted", pending[0].Error)
			fetched := agent.KnowledgeReadResult{MemoryState: state, Memo: &knowledge.Memo{Text: "private train note"}}
			reads, err := f.host.CompleteKnowledge(ctx, "alice", 17911, index, fetched)
			require.NoError(t, err)
			require.Equal(t, request, reads[0].Request)
			require.Equal(t, fetched.Memo, reads[0].Memo)
			_, err = service.Execute(ctx, "alice", knowledge.Command{
				Name: knowledge.MemoDelete, Key: "memory-state-delete", FactKey: "train", Version: 1,
			})
			require.NoError(t, err)
			require.NoError(t, f.host.ReconcileMemory(ctx, "alice", state))
			reads, err = f.host.CompleteKnowledge(ctx, "alice", 17911, index, fetched)
			require.NoError(t, err)
			require.True(t, reads[0].Omitted)
			require.Nil(t, reads[0].Memo)
			persisted, err := store.Knowledge(ctx, "alice", 17911)
			require.NoError(t, err)
			require.Equal(t, reads, persisted)
			_, err = f.fixture.b.DB.Exec(ctx, "SELECT body FROM core.knowledge_memos LIMIT 1")
			var denied *pgconn.PgError
			require.ErrorAs(t, err, &denied)
			require.Equal(t, "42501", denied.Code)
			_, err = f.fixture.b.DB.Exec(ctx, "SELECT text FROM core.conversation_events LIMIT 1")
			require.ErrorAs(t, err, &denied)
			require.Equal(t, "42501", denied.Code)
		})
	}
}

func TestMemoryReadStateAuthorizationAndBounds(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"local", "http"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newMemoryReadStateFixture(t, mode == "local")
			wrongOwner := f.host
			wrongOwner.UserToken = func(ctx context.Context, _ string) (string, error) {
				return f.host.UserToken(ctx, "alice")
			}
			_, err := wrongOwner.ReserveKnowledge(t.Context(), "bob", 17912, agent.KnowledgeProposal{})
			require.Error(t, err)
			_, err = f.host.ReserveKnowledge(t.Context(), "alice", 0, agent.KnowledgeProposal{})
			require.Error(t, err)
			_, err = f.host.CompleteKnowledge(t.Context(), "alice", 17912, -1, agent.KnowledgeReadResult{})
			require.Error(t, err)
			_, err = f.host.ReserveKnowledge(t.Context(), "alice", 17912,
				agent.KnowledgeProposal{Text: strings.Repeat("x", appclient.MaxAPIBytes+1)})
			require.Error(t, err)
			f.revoked.Store(true)
			_, err = f.host.ReserveKnowledge(t.Context(), "alice", 17912, agent.KnowledgeProposal{})
			require.Error(t, err)
			var count int
			require.NoError(t, f.fixture.db.QueryRow(t.Context(),
				"SELECT count(*) FROM bot.interactions WHERE update_id=17912 AND kind='knowledge_reads'").Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestMemoryReadStateRequiresHostCredential(t *testing.T) {
	t.Parallel()
	f := newMemoryReadStateFixture(t, false)
	token, err := f.host.UserToken(t.Context(), "alice")
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		f.host.Base+"/internal/memory/read-state/reserve", strings.NewReader(`{"update_id":17913}`))
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
}

func TestMemoryReadStateFailureOrigins(t *testing.T) {
	t.Parallel()
	f := newMemoryReadStateFixture(t, true)
	provider := f.host
	provider.UserToken = func(context.Context, string) (string, error) { return "", io.EOF }
	_, err := provider.ReserveKnowledge(t.Context(), "alice", 17914, agent.KnowledgeProposal{})
	require.ErrorIs(t, err, io.EOF)
	require.False(t, core.IsDatabaseFailure(err))
	config := f.application.Config()
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	broken, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(broken.Close)
	f.host.LocalMemoryReadState.Service = agenthost.MemoryReadStore{DB: broken}
	_, err = f.host.ReserveKnowledge(t.Context(), "alice", 17914, agent.KnowledgeProposal{})
	require.ErrorIs(t, err, core.ErrDatabase)
	f.host.LocalMemoryReadState.Service = agenthost.MemoryReadStore{DB: f.application}
	_, err = f.fixture.db.Exec(t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',17914,'knowledge_reads','{}')`)
	require.NoError(t, err)
	_, err = f.host.ReserveKnowledge(t.Context(), "alice", 17914, agent.KnowledgeProposal{})
	require.Error(t, err)
	require.False(t, core.IsDatabaseFailure(err))
}

func TestMemoryReadStateReconciliationRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"local", "http"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newMemoryReadStateFixture(t, mode == "local")
			for _, kind := range []string{"knowledge_reads", "script_runs"} {
				for _, raw := range []string{`{}`, `42`, `null`} {
					checkMalformedMemoryReconciliation(t, f, kind, raw)
				}
			}
			broken := memoryReadStateFailureHost(t, f)
			err := broken.ReconcileMemory(t.Context(), "alice", knowledge.MemoryDeletionState{})
			require.ErrorIs(t, err, core.ErrDatabase)
		})
	}
}

func TestMemoryReadStateNullCannotResetReservation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"local", "http"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newMemoryReadStateFixture(t, mode == "local")
			_, err := f.fixture.db.Exec(
				t.Context(),
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',17918,'knowledge_reads','null')`,
			)
			require.NoError(t, err)
			_, err = f.host.ReserveKnowledge(t.Context(), "alice", 17918, agent.KnowledgeProposal{})
			require.Error(t, err)
			require.False(t, core.IsDatabaseFailure(err))
			assertMemoryNullRetained(t, f)
			_, err = f.host.CompleteKnowledge(t.Context(), "alice", 17918, 0, agent.KnowledgeReadResult{})
			require.Error(t, err)
			require.False(t, core.IsDatabaseFailure(err))
			assertMemoryNullRetained(t, f)
		})
	}
}

func assertMemoryNullRetained(t *testing.T, f *memoryReadStateFixture) {
	t.Helper()
	var content string
	require.NoError(t, f.fixture.db.QueryRow(
		t.Context(),
		`SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=17918 AND kind='knowledge_reads'`,
	).
		Scan(&content))
	require.Equal(t, "null", content)
}

func checkMalformedMemoryReconciliation(t *testing.T, f *memoryReadStateFixture, kind, raw string) {
	t.Helper()
	_, err := f.fixture.db.Exec(t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',17915,$1,$2::jsonb)`, kind, raw)
	require.NoError(t, err)
	err = f.host.ReconcileMemory(t.Context(), "alice", knowledge.MemoryDeletionState{})
	require.Error(t, err, "kind=%s content=%s", kind, raw)
	require.False(t, core.IsDatabaseFailure(err))
	var retained string
	require.NoError(t, f.fixture.db.QueryRow(
		t.Context(),
		`SELECT content::text FROM bot.interactions WHERE owner='alice' AND update_id=17915 AND kind=$1`,
		kind,
	).Scan(&retained))
	require.JSONEq(t, raw, retained)
	_, err = f.fixture.db.Exec(t.Context(),
		`DELETE FROM bot.interactions WHERE owner='alice' AND update_id=17915 AND kind=$1`, kind)
	require.NoError(t, err)
}

func memoryReadStateFailureHost(t *testing.T, f *memoryReadStateFixture) appclient.Host {
	t.Helper()
	config := f.application.Config()
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.EOF }
	broken, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(broken.Close)
	host := f.host
	if host.LocalMemoryReadState != nil {
		local := *host.LocalMemoryReadState
		local.Service = agenthost.MemoryReadStore{DB: broken}
		host.LocalMemoryReadState = &local
		return host
	}
	services := appservices.NewServices(f.application, appservices.Options{})
	services.MemoryReadState = agenthost.MemoryReadStore{DB: broken}
	server := httptest.NewServer(api.Handler(services, host.Signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	host.Base = server.URL
	return host
}

func TestMemoryReadStateAggregateBudgetBeforeCommit(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"local", "http"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newMemoryReadStateFixture(t, mode == "local")
			checkMemoryAggregate(t, f, 17916, 400<<10, false)
			checkMemoryAggregate(t, f, 17917, 600<<10, true)
		})
	}
}

func checkMemoryAggregate(t *testing.T, f *memoryReadStateFixture, update int64, size int, refuse bool) {
	t.Helper()
	for range agent.MaxKnowledgeReads {
		_, err := f.host.ReserveKnowledge(t.Context(), "alice", update, agent.KnowledgeProposal{})
		require.NoError(t, err)
	}
	result := agent.KnowledgeReadResult{Memo: &knowledge.Memo{Text: strings.Repeat("x", size)}}
	_, err := f.host.CompleteKnowledge(t.Context(), "alice", update, 0, result)
	require.NoError(t, err)
	reader := agenthost.ReadStore{DB: f.fixture.b.DB}
	before, err := reader.Knowledge(t.Context(), "alice", update)
	require.NoError(t, err)
	completed, err := f.host.CompleteKnowledge(t.Context(), "alice", update, 1, result)
	if refuse {
		var problem *core.ProblemError
		require.ErrorAs(t, err, &problem)
		require.Equal(t, http.StatusRequestEntityTooLarge, problem.Status)
		require.False(t, core.IsDatabaseFailure(err))
		require.Nil(t, completed)
	} else {
		require.NoError(t, err)
		require.Len(t, completed, agent.MaxKnowledgeReads)
		require.Equal(t, result.Memo, completed[1].Memo)
	}
	stored, err := reader.Knowledge(t.Context(), "alice", update)
	require.NoError(t, err)
	if refuse {
		require.Equal(t, before, stored)
		require.Equal(t, "interrupted", stored[1].Error)
	} else {
		require.Equal(t, completed, stored)
	}
}
