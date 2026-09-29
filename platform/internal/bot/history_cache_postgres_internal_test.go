package bot

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestHistoryCachePostgresDeletionAndCompletion(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	archive := conversation.Service{DB: db}
	require.NoError(t, archive.AppendOriginal(t.Context(), "alice", "cache-proof", "user", "private history canary"))
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
		case "/internal/history/authority":
			historyAuthorityFixture(w, r, archive)
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
		API: appclient.Client{
			Base:     server.URL,
			Exchange: &authExchange{},
			Links: authLinks{
				user: identity.User{Owner: "alice", Subject: "z-alice"},
			},
			SandboxToken: (identity.Signer{}).Token,
		},
	}
	b.Host = appclient.Host{Base: server.URL, UserToken: b.API.UserToken}
	ctx, owner, err := b.API.AuthenticateTelegram(t.Context(), 101)
	require.NoError(t, err)
	index, err := b.scriptHost().Store.ReserveRun(
		ctx,
		owner,
		701,
		agent.ScriptProposal{Code: "return 'private history canary';"},
		0,
		nil,
		[]readsource.Authority{},
		false,
	)
	require.NoError(t, err)
	call := agenthost.ScriptToolRecord{
		Outcome: agent.ScriptToolResult{
			Name:   scriptHistoryRead,
			Result: json.RawMessage(`{"text":"private history canary"}`),
		},
	}
	_, err = b.scriptHost().Store.AdmitCall(ctx, owner, 701, index, &call)
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
	older := []agenthost.ScriptRecord{
		{Run: agent.ScriptRun{Result: json.RawMessage(`{"derived":"private history canary"}`)}},
	}
	_, err = db.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,699,'script_runs',$2)`,
		owner,
		older,
	)
	require.NoError(t, err)
	plan := interaction.SavedPlan{FormatVersion: interaction.CurrentFormatVersion,
		Kind:  interaction.DerivedPlan,
		State: interaction.Ready,
		Plan:  agent.Plan{Text: "private history canary"},
		PassAuthority: &interaction.PlanAuthority{
			Reads:           []interaction.PassContextDependency{},
			ReadAuthorities: []readsource.Authority{},
		},
	}
	_, err = (interaction.Store{DB: db}).SaveWinner(ctx, owner, 702, plan)
	require.NoError(t, err)
	barrier, err := db.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = barrier.Rollback(ctx) }()
	require.NoError(t, agenthost.LockScript(ctx, barrier, owner, 701))
	finishing.Store(true)
	completed := make(chan error, 1)
	go func() {
		_, finishErr := b.scriptHost().Store.CompleteRun(
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
	require.ErrorIs(t, <-completed, appclient.ErrReadStale)
	records, err := b.scriptHost().Store.Records(ctx, owner, 701)
	require.NoError(t, err)
	require.True(t, records[0].HistoryRedacted)
	// A late tool completion must not restore its raw payload after reconciliation.
	require.ErrorIs(t, b.scriptHost().Store.CompleteCall(ctx, owner, 701, index, 0, call), appclient.ErrReadStale)
	var count int
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT count(*) FROM bot.interactions WHERE owner=$1 AND content::text LIKE '%private history canary%'`, owner).
			Scan(&count),
	)
	require.Zero(t, count)
	loaded, err := b.readStore().History(ctx, owner, 700)
	require.NoError(t, err)
	require.Equal(t, historyDeleted, loaded[0].Error)
	require.Empty(t, loaded[0].Events)
	_, err = b.planForUpdate(ctx, incoming{owner: owner}, 702)
	require.ErrorIs(t, err, appclient.ErrReadStale)
	stored, err := (interaction.Store{DB: db}).Load(ctx, owner, 702)
	require.NoError(t, err)
	require.Equal(t, interaction.HistoryDeleted, stored.TerminalReason)
	require.Empty(t, stored.Plan.Text)
	_, err = b.planForUpdate(ctx, incoming{owner: owner}, 702)
	require.ErrorIs(t, err, appclient.ErrReadStale)
	_, err = b.executePlan(ctx, incoming{owner: owner}, 702, plan)
	require.ErrorIs(t, err, appclient.ErrReadStale)
}

func historyAuthorityFixture(w http.ResponseWriter, r *http.Request, archive conversation.Service) {
	var input struct {
		ReadAuthorities []readsource.Authority `json:"read_authorities"`
	}
	if json.NewDecoder(r.Body).Decode(&input) != nil {
		http.Error(w, "invalid authority", http.StatusBadRequest)
		return
	}
	err := archive.CheckReadAuthorities(r.Context(), "alice", input.ReadAuthorities)
	if err != nil {
		if problem, ok := errors.AsType[*core.ProblemError](err); ok {
			w.WriteHeader(problem.Status)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": problem.Code})
			return
		}
		http.Error(w, "authority unavailable", http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
