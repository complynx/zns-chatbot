package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestModernQuoteMetadataPreparation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{
		"valid", "foreign", "invalid_ref", "missing_call", "wrong_ref", "consumed", "failed",
		"missing_result", "null_result", "pass_redacted", "memory_redacted", "missing_generation", "generation_changed", "redacted", "expired", "event_bound",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db := foodPendingDatabase(t)
			bot := &Bot{DB: db, API: modernMetadataGenerationClient(t)}
			store := agenthost.ScriptStore{DB: db, StaleError: appclient.ErrReadStale}

			generation := int64(1)
			record := agenthost.ScriptRecord{HistoryGeneration: generation, Calls: []agenthost.ScriptToolRecord{{
				ModernChoice: &agenthost.ModernChoiceRecord{
					Catalog: strings.Repeat(
						"a",
						64,
					),
					Ref:               "94001.0.0",
					Event:             "sandbox-festival",
					HistoryGeneration: &generation,
				},
				Outcome: agent.ScriptToolResult{Result: json.RawMessage(`{}`)},
			}}}
			mutateModernMetadataFixture(scenario, &record)
			_, err := db.Exec(t.Context(), `INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES('alice',94001,'script_runs',$1) ON CONFLICT(owner,update_id,kind)
DO UPDATE SET content=excluded.content,created_at=clock_timestamp()`, []agenthost.ScriptRecord{record})
			require.NoError(t, err)
			if scenario == "expired" {
				_, err = db.Exec(
					t.Context(),
					`UPDATE bot.interactions SET created_at=clock_timestamp()-interval '25 hours' WHERE owner='alice' AND update_id=94001`,
				)
				require.NoError(t, err)
			}
			owner, ref := "alice", "94001.0.0"
			switch scenario {
			case "foreign":
				owner = "bob"
			case "invalid_ref":
				ref = "invalid"
			case "missing_call":
				ref = "94001.0.1"
			}
			metadata, err := store.LoadModernChoiceMetadata(t.Context(), owner, ref)
			if scenario != "valid" {
				require.Error(t, err)
				require.Empty(t, metadata.Event)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "sandbox-festival", metadata.Event)
			prepared, err := bot.prepareModernOrderTool(
				t.Context(),
				owner,
				94002,
				scriptclient.ToolCall{
					Name:      modernOrdersQuote,
					Arguments: json.RawMessage(`{"choice_ref":"94001.0.0"}`),
				},
				agent.Input{},
			)
			require.NoError(t, err, "preparation checks generation without hydrating choice or catalog")
			require.Equal(t, metadata.Event, prepared.ModernOrder.Event)
		})
	}
}

func mutateModernMetadataFixture(scenario string, record *agenthost.ScriptRecord) {
	call := &record.Calls[0]
	switch scenario {
	case "wrong_ref":
		call.ModernChoice.Ref = "94001.0.2"
	case "consumed":
		call.ModernChoice.Child = "94002.0.0"
	case "failed":
		call.Outcome.Error = "interrupted"
	case "null_result":
		call.Outcome.Result = json.RawMessage("null")
	case "pass_redacted":
		record.PassRedacted = true
	case "memory_redacted":
		record.MemoryRedacted = true
	case "missing_result":
		call.Outcome.Result = nil
	case "missing_generation":
		call.ModernChoice.HistoryGeneration = nil
	case "generation_changed":
		record.HistoryGeneration++
	case "redacted":
		record.HistoryRedacted = true
	case "event_bound":
		call.ModernChoice.Event = strings.Repeat("e", 129)
	}
}

func modernMetadataGenerationClient(t *testing.T) appclient.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/order-events/sandbox-festival/choice-snapshot" {
			_, err := w.Write([]byte(`{"catalog":"` + strings.Repeat("a", 64) + `"}`))
			assert.NoError(t, err)
			return
		}
		if r.URL.Path != "/v1/me/history/generation" {
			http.Error(w, "unexpected domain hydration", http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(`{"generation":1}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	return appclient.Client{
		Base:         server.URL,
		HTTP:         server.Client(),
		SandboxToken: func(string) string { return "synthetic" },
	}
}
