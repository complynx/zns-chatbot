package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestScriptAuthorityBatchKeepsEveryDependency(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"duplicate", "unique_revoked", "oversized", "oversized_revoked"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			var requests atomic.Int32
			var leaves atomic.Int32
			server := httptest.NewServer(batchAuthorityHandler(&requests, &leaves))
			t.Cleanup(server.Close)
			b := Bot{
				Host: appclient.Host{
					Base:      server.URL,
					UserToken: func(context.Context, string) (string, error) { return "live-user", nil },
				},
			}
			record := batchAuthorityRecord(scenario)
			changed, err := (agenthost.ScriptAuthorization{ScriptDomainAuthority: botScriptAuthority{bot: &b}}).AccessChanged(
				t.Context(),
				"alice",
				record,
			)
			require.NoError(t, err)
			require.Equal(t, scenario == "unique_revoked" || scenario == "oversized_revoked", changed)
			expected := int32(1)
			if scenario == "oversized" || scenario == "oversized_revoked" {
				expected = 2
			}
			require.Equal(t, expected, requests.Load())
			if scenario == "oversized" {
				require.EqualValues(t, 400, leaves.Load(), "fallback validates every leaf")
			}
			if scenario == "duplicate" {
				require.EqualValues(t, 1, leaves.Load())
				_, err = (agenthost.ScriptAuthorization{ScriptDomainAuthority: botScriptAuthority{bot: &b}}).AccessChanged(
					t.Context(),
					"alice",
					record,
				)
				require.NoError(t, err)
				require.EqualValues(t, 2, requests.Load(), "next boundary must check again")
			}
		})
	}
}

func TestInspectCompletionKeepsFreshAuthorityAndConcurrentProgress(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"allowed", "revoked", "deleted", "revised"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			const updateID int64 = 49812
			record := batchAuthorityRecord("duplicate")
			record.Calls = record.Calls[:2]
			record.Calls[0].Outcome.Result = json.RawMessage(`{"value":"private-order"}`)
			pending := record.Calls[1]
			pending.Outcome.Result = nil
			pending.Outcome.Error = "interrupted"
			record.Calls[1] = pending
			_, err := db.Exec(t.Context(),
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',$1,'script_runs',$2)`,
				updateID, []agenthost.ScriptRecord{record})
			require.NoError(t, err)
			mutate := func(ctx context.Context) error {
				var fixtureErr error
				switch scenario {
				case "deleted":
					_, fixtureErr = db.Exec(
						ctx,
						`DELETE FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`,
						updateID,
					)
				case "revised":
					_, fixtureErr = db.Exec(
						ctx,
						`UPDATE bot.interactions SET content=jsonb_set(content,'{0,calls,0,outcome,result}',$2::jsonb)
 WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`,
						updateID,
						`{"value":"concurrent-private-order"}`,
					)
				}
				return fixtureErr
			}
			server := httptest.NewServer(inspectCompletionAuthorityHandler(scenario, mutate))
			t.Cleanup(server.Close)
			token := func(context.Context, string) (string, error) { return "live-user", nil }
			b := Bot{
				DB: db,
				API: appclient.Client{
					Base:         server.URL,
					SandboxToken: func(string) string { return "live-user" },
				},
				Host: appclient.Host{Base: server.URL, UserToken: token},
			}
			completion := pending
			completion.Outcome.Error = ""
			completion.Outcome.Result = json.RawMessage(`{"value":"complete-order"}`)
			err = b.scriptHost().Store.CompleteCall(t.Context(), "alice", updateID, 0, 1, completion)
			if scenario == "deleted" {
				require.Error(t, err)
				var rows int
				require.NoError(
					t,
					db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, updateID).
						Scan(&rows),
				)
				require.Zero(t, rows, "a removed admission cannot be resurrected")
				return
			}
			var saved []byte
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, updateID).
					Scan(&saved),
			)
			if scenario == "revoked" {
				require.Error(t, err)
				require.NotContains(t, string(saved), "private-order")
				require.NotContains(t, string(saved), "complete-order")
				return
			}
			require.NoError(t, err)
			require.Contains(t, string(saved), "complete-order")
			if scenario == "revised" {
				require.Contains(t, string(saved), "concurrent-private-order")
			} else {
				require.Contains(t, string(saved), "private-order")
			}
		})
	}
}

func inspectCompletionAuthorityHandler(scenario string, mutate func(context.Context) error) http.HandlerFunc {
	var changed atomic.Bool
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/me/history/generation" {
			_ = json.NewEncoder(w).Encode(map[string]int64{"generation": 0})
			return
		}
		var input struct {
			ReadAuthorities []readsource.Authority `json:"read_authorities"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !readsource.Valid(input.ReadAuthorities) {
			http.Error(w, "invalid evidence", http.StatusBadRequest)
			return
		}
		if scenario == "revoked" {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "history_stale"})
			return
		}
		if changed.CompareAndSwap(false, true) {
			if err := mutate(r.Context()); err != nil {
				http.Error(w, "fixture update failed", http.StatusInternalServerError)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

func TestPlanAuthorityKeepsUnmatchedCarriersAndFreshRevisions(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"allowed", "raw_revoked", "covered_revised_revoked", "unmatched_revoked", "revised_revoked", "custom_revoked", "legacy"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			const updateID int64 = 49813
			record := batchAuthorityRecord("duplicate")
			record.Calls = record.Calls[:2]
			refs, err := agenthost.ScriptReadAuthorities("alice", []agenthost.ScriptRecord{record})
			require.NoError(t, err)
			if scenario == "unmatched_revoked" || scenario == "revised_revoked" {
				record.Calls = append(record.Calls, batchAuthorityCall([]readsource.Authority{
					{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "extra"}},
				}))
			}
			if scenario == "legacy" {
				record.PassContext = nil
			}
			_, err = db.Exec(t.Context(),
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',$1,'script_runs',$2)`,
				updateID, []agenthost.ScriptRecord{record})
			require.NoError(t, err)
			var revoked atomic.Bool
			if scenario == "raw_revoked" {
				config := db.Config()
				config.ConnConfig.Tracer = planRawRevocation{revoked: &revoked}
				db, err = pgxpool.NewWithConfig(t.Context(), config)
				require.NoError(t, err)
				t.Cleanup(db.Close)
			}
			mutate := func(ctx context.Context) error {
				_, updateErr := db.Exec(
					ctx,
					`UPDATE bot.interactions SET content=jsonb_set(content,'{0,run,error}','"revised"'::jsonb)
 WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`,
					updateID,
				)
				return updateErr
			}
			server := httptest.NewServer(planBoundaryAuthorityHandler(scenario, &revoked, mutate))
			t.Cleanup(server.Close)
			b := Bot{DB: db, Host: appclient.Host{Base: server.URL,
				UserToken: func(context.Context, string) (string, error) { return "live-user", nil }}}
			domain := botScriptAuthority{bot: &b}
			ledgerDomain := domain
			if scenario == "custom_revoked" {
				var denied atomic.Bool
				denied.Store(true)
				other := httptest.NewServer(planBoundaryAuthorityHandler(scenario, &denied, mutate))
				t.Cleanup(other.Close)
				otherBot := b
				otherBot.Host.Base = other.URL
				ledgerDomain = botScriptAuthority{bot: &otherBot}
			}
			policy := agenthost.PlanAuthorization{
				DB:      db,
				Sources: domain,
				Scripts: agenthost.ScriptStore{
					DB:     db,
					Policy: agenthost.ScriptAuthorization{ScriptDomainAuthority: ledgerDomain},
				},
			}
			authority := &interaction.PlanAuthority{
				Reads:           []interaction.PassContextDependency{},
				ReadAuthorities: refs,
				Scripts:         true,
			}
			changed, err := policy.Changed(t.Context(), "alice", updateID, authority)
			require.NoError(t, err)
			require.Equal(t, scenario != "allowed", changed)
			assertPlanBoundaryPersistence(t, scenario, db, policy, authority, &revoked, updateID)
		})
	}
}

func assertPlanBoundaryPersistence(t *testing.T, scenario string, db *pgxpool.Pool,
	policy agenthost.PlanAuthorization, authority *interaction.PlanAuthority, revoked *atomic.Bool, updateID int64) {
	t.Helper()
	switch scenario {
	case "raw_revoked":
		return
	case "allowed":
		revoked.Store(true)
		changed, err := policy.Changed(t.Context(), "alice", updateID, authority)
		require.NoError(t, err)
		require.True(t, changed, "a later validation observes current revocation")
	default:
		var saved []byte
		require.NoError(t, db.QueryRow(t.Context(),
			`SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`,
			updateID).Scan(&saved))
		if scenario == "covered_revised_revoked" {
			require.Contains(
				t,
				string(saved),
				"revised",
				"concurrent progress is retained while current authority denies release",
			)
			return
		}
		require.NotContains(t, string(saved), "body", "invalid carriers are durably retired")
	}
}

func planBoundaryAuthorityHandler(
	scenario string,
	revoked *atomic.Bool,
	mutate func(context.Context) error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ReadAuthorities []readsource.Authority `json:"read_authorities"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !readsource.Valid(input.ReadAuthorities) {
			http.Error(w, "invalid evidence", http.StatusBadRequest)
			return
		}
		changed, err := planBoundaryDecision(r.Context(), scenario, revoked, mutate, input.ReadAuthorities)
		if err != nil {
			http.Error(w, "fixture update failed", http.StatusInternalServerError)
			return
		}
		if changed {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "history_stale"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}

func planBoundaryDecision(ctx context.Context, scenario string, revoked *atomic.Bool,
	mutate func(context.Context) error, refs []readsource.Authority) (bool, error) {
	if len(refs) > 0 && scenario == "covered_revised_revoked" && revoked.CompareAndSwap(false, true) {
		return false, mutate(ctx)
	}
	for _, ref := range refs {
		if ref.Knowledge.Scope == "extra" && scenario == "revised_revoked" && revoked.CompareAndSwap(false, true) {
			if err := mutate(ctx); err != nil {
				return false, err
			}
		}
		if ref.Knowledge.Scope == "extra" && scenario == "unmatched_revoked" ||
			ref.Knowledge.Scope == "same" && revoked.Load() {
			return true, nil
		}
	}
	return false, nil
}

// Revocation becomes visible while the current raw database read is admitted.
// The plan must authorize after that read, not retain its earlier decision.
type planRawRevocation struct{ revoked *atomic.Bool }

func (trace planRawRevocation) TraceQueryStart(
	ctx context.Context,
	_ *pgx.Conn,
	_ pgx.TraceQueryStartData,
) context.Context {
	trace.revoked.Store(true)
	return ctx
}

func (planRawRevocation) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func batchAuthorityCall(refs []readsource.Authority) agenthost.ScriptToolRecord {
	generation := int64(0)
	return agenthost.ScriptToolRecord{
		Source:            &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
		ResultAuthorities: refs,
		Outcome:           agent.ScriptToolResult{Name: "orders.inspect", Result: json.RawMessage(`{"value":"body"}`)},
	}
}

func batchAuthorityRecord(scenario string) agenthost.ScriptRecord {
	record := agenthost.ScriptRecord{
		PassContext:     []interaction.PassContextDependency{},
		ReadAuthorities: []readsource.Authority{},
	}
	switch scenario {
	case "duplicate", "unique_revoked":
		for range 8 {
			record.Calls = append(
				record.Calls,
				batchAuthorityCall(
					[]readsource.Authority{
						{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "same"}},
					},
				),
			)
		}
		if scenario == "unique_revoked" {
			record.Calls = append(
				record.Calls,
				batchAuthorityCall(
					[]readsource.Authority{
						{
							Knowledge: knowledgeauthority.ReadAuthority{
								Kind:  knowledgeauthority.Review,
								Scope: "revoked",
							},
						},
					},
				),
			)
		}
	default:
		for group := range 2 {
			refs := []readsource.Authority{}
			for n := range 200 {
				refs = append(
					refs,
					readsource.Authority{
						Knowledge: knowledgeauthority.ReadAuthority{
							Kind:  knowledgeauthority.Review,
							Scope: strconv.Itoa(group*200 + n),
						},
					},
				)
			}
			if scenario == "oversized_revoked" && group == 1 {
				refs[0].Knowledge.Scope = "revoked"
			}
			record.Calls = append(record.Calls, batchAuthorityCall(refs))
		}
	}
	return record
}

func batchAuthorityHandler(requests, leaves *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ReadAuthorities []readsource.Authority `json:"read_authorities"`
		}
		if json.NewDecoder(r.Body).Decode(&input) != nil || !readsource.Valid(input.ReadAuthorities) {
			http.Error(w, "invalid evidence", http.StatusBadRequest)
			return
		}
		requests.Add(1)
		for _, a := range input.ReadAuthorities {
			if a.Knowledge.Kind == knowledgeauthority.Review {
				leaves.Add(1)
				if a.Knowledge.Scope == "revoked" {
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]string{"code": "history_stale"})
					return
				}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
}
