package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestHistoryCachePostgresDeletionAndCompletion(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	archive := conversation.Service{DB: db}
	require.NoError(t, archive.Append(t.Context(), "alice", "cache-proof", "user", "private history canary"))
	var eventID int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT id FROM core.conversation_events WHERE owner='alice' AND source_key='cache-proof'`).
			Scan(&eventID),
	)
	var finishing atomic.Bool
	completionStarted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/me/history/generation":
			generation, err := archive.Generation(r.Context(), "alice")
			if err != nil {
				http.Error(w, "generation unavailable", http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]int64{"generation": generation})
		case "/v1/memory/deletions":
			if finishing.Load() {
				select {
				case completionStarted <- struct{}{}:
				default:
				}
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	b := Bot{
		DB: db,
		API: APIClient{
			Base:     server.URL,
			Exchange: &authExchange{},
			Links:    authLinks{user: identity.User{Owner: "alice", Subject: "z-alice"}},
		},
	}
	ctx, owner, err := b.API.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	index, err := b.reserveScript(ctx, owner, 701, agent.ScriptProposal{Code: "return 'private history canary';"}, 0)
	require.NoError(t, err)
	call := scriptToolRecord{
		Outcome: agent.ScriptToolResult{
			Name:   scriptHistoryRead,
			Result: json.RawMessage(`{"text":"private history canary"}`),
		},
	}
	_, err = b.reserveScriptTool(ctx, owner, 701, index, &call)
	require.NoError(t, err)
	pages := []conversation.Page{
		{Generation: 0, Events: []conversation.Event{{ID: eventID, Text: "private history canary"}}},
	}
	_, err = db.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,700,'history_reads',$2)`,
		owner,
		pages,
	)
	require.NoError(t, err)
	older := []scriptRecord{{Run: agent.ScriptRun{Result: json.RawMessage(`{"derived":"private history canary"}`)}}}
	_, err = db.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,699,'script_runs',$2)`,
		owner,
		older,
	)
	require.NoError(t, err)
	plan := cachedPlan{Plan: agent.Plan{Text: "private history canary"}}
	_, err = db.Exec(ctx, `INSERT INTO bot.replies(update_id,plan) VALUES(702,$1)`, plan)
	require.NoError(t, err)
	barrier, err := db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback(ctx) }()
	require.NoError(t, scriptLock(ctx, barrier, owner, 701))
	finishing.Store(true)
	completed := make(chan error, 1)
	go func() {
		_, finishErr := b.finishScript(
			ctx,
			owner,
			701,
			index,
			agent.ScriptRun{Result: json.RawMessage(`{"derived":"late private history canary"}`)},
		)
		completed <- finishErr
	}()
	select {
	case <-completionStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	require.NoError(t, archive.DeleteContent(ctx, owner, eventID))
	require.NoError(t, barrier.Commit(ctx))
	require.NoError(t, <-completed)
	records, err := b.scriptRecords(ctx, owner, 701)
	require.NoError(t, err)
	require.True(t, records[0].HistoryRedacted)
	// A late tool completion must not restore its raw payload after reconciliation.
	require.NoError(t, b.finishScriptTool(ctx, owner, 701, index, 0, call))
	var count int
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT count(*) FROM bot.interactions WHERE owner=$1 AND content::text LIKE '%private history canary%'`, owner).
			Scan(&count),
	)
	require.Zero(t, count)
	loaded, err := b.historyReads(ctx, owner, 700)
	require.NoError(t, err)
	require.Equal(t, historyDeleted, loaded[0].Error)
	require.Empty(t, loaded[0].Events)
	_, err = b.planForUpdate(ctx, incoming{owner: owner}, 702)
	require.ErrorIs(t, err, errScriptReadStale)
	var stored cachedPlan
	require.NoError(t, db.QueryRow(ctx, `SELECT plan FROM bot.replies WHERE update_id=702`).Scan(&stored))
	require.True(t, stored.HistoryRedacted)
	require.Empty(t, stored.Plan.Text)
	_, err = b.planForUpdate(ctx, incoming{owner: owner}, 702)
	require.ErrorIs(t, err, errScriptReadStale)
	_, err = b.executePlan(ctx, incoming{owner: owner}, 702, plan)
	require.ErrorIs(t, err, errScriptReadStale)
}
