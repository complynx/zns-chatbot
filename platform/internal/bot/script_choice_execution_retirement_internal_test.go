package bot

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestModernQuoteParentRetiredAfterPreparation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"available", "history_redacted", "memory_redacted", "pass_redacted", "null_result"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			bot, calls := quoteRetirementFixture(t)
			call := scriptclient.ToolCall{
				Name:      modernOrdersQuote,
				Arguments: json.RawMessage(`{"choice_ref":"94001.0.0"}`),
			}
			prepared, err := bot.prepareModernOrderTool(t.Context(), "alice", 94002, call, agent.Input{})
			require.NoError(t, err)
			if scenario != "available" {
				path, value := []string{"0", scenario}, "true"
				if scenario == "null_result" {
					path = []string{"0", "calls", "0", "outcome", "result"}
					value = "null"
				}
				tag, writeErr := bot.DB.Exec(
					t.Context(),
					`UPDATE bot.interactions SET content=jsonb_set(content,$1::text[],$2::jsonb,true) WHERE owner='alice' AND update_id=94001 AND kind='script_runs'`,
					path,
					value,
				)
				require.NoError(t, writeErr)
				require.EqualValues(t, 1, tag.RowsAffected())
			}
			calls.Store(0)
			result, err := bot.executeModernOrderTool(t.Context(), "alice", call, prepared, &agent.Input{})
			if scenario == "available" {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Positive(t, calls.Load())
			} else {
				require.Error(t, err)
				assert.Nil(t, result, "retired content must not become a quote result")
				assert.Zero(t, calls.Load(), "execution must reject the parent before domain dispatch")
			}
			var effects int
			require.NoError(
				t,
				bot.DB.QueryRow(t.Context(), `SELECT count(*) FROM core.order_operations`).Scan(&effects),
			)
			require.Zero(t, effects)
		})
	}
}

func quoteRetirementFixture(t *testing.T) (*Bot, *atomic.Int64) {
	t.Helper()
	db := foodPendingDatabase(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	var calls atomic.Int64
	handler := api.Handler(appservices.NewServices(db, appservices.Options{}), signer, slog.New(slog.DiscardHandler))
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); handler.ServeHTTP(w, r) }),
	)
	t.Cleanup(server.Close)
	bot := &Bot{DB: db, API: appclient.Client{Base: server.URL, SandboxToken: signer.Token, HTTP: server.Client()}}
	event, err := bot.API.OrderEvent(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	choice, err := bot.API.QuoteOrder(
		t.Context(),
		"alice",
		event.ID,
		orders.ChoiceInput{Customer: "Synthetic retained choice"},
	)
	require.NoError(t, err)
	generation, err := bot.API.HistoryGeneration(t.Context(), "alice")
	require.NoError(t, err)
	records := []agenthost.ScriptRecord{{HistoryGeneration: generation, Calls: []agenthost.ScriptToolRecord{
		{
			ModernChoice: &agenthost.ModernChoiceRecord{
				Ref:               "94001.0.0",
				Event:             event.ID,
				Catalog:           modernCatalogFingerprint(event),
				Choice:            choice,
				HistoryGeneration: &generation,
			},
			Outcome: agent.ScriptToolResult{Name: modernOrdersChoice, Result: json.RawMessage(`{}`)},
		},
	}}}
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',94001,'script_runs',$1)`,
		records,
	)
	require.NoError(t, err)
	return bot, &calls
}
