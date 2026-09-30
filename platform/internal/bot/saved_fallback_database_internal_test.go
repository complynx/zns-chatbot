package bot

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func r36Source() readsource.Derivation {
	generation := int64(0)
	return readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}}
}

func TestSavedScriptFallbackPreservesDatabaseCause(t *testing.T) {
	t.Parallel()
	for _, marked := range []bool{false, true} {
		transport := &r36FailureTransport{status: http.StatusConflict, code: "stale_model_settings", marked: marked}
		b := r36FailureBot(transport)
		_, err := b.executeScriptModel(t.Context(), "alice", scriptclient.ToolCall{}, agenthost.ScriptToolRecord{
			Model: &agenthost.ScriptModelRequest{Endpoint: "/v1/model-settings"},
		}, nil)
		r36AssertStale(t, marked, err)
		require.Equal(t, 1, transport.calls)
		transport.code = "pass_profile_stale"
		source := r36Source()
		input := &agent.Input{}
		record := agenthost.ScriptToolRecord{
			Source: &source, Profile: &agenthost.ScriptProfileMutation{Field: "role", Value: "leader", Key: "r36"},
		}
		_, err = b.executeScriptProfileMutation(
			t.Context(),
			"alice",
			scriptclient.ToolCall{Name: scriptProfileSet},
			record,
			input,
		)
		r36AssertStale(t, marked, err)
		require.Nil(t, input.Profile)
		require.Equal(t, 2, transport.calls)
	}
}

func r36AssertStale(t *testing.T, marked bool, err error) {
	t.Helper()
	if marked {
		require.ErrorIs(t, err, core.ErrDatabase)
		require.NotErrorIs(t, err, appclient.ErrReadStale)
	} else {
		require.ErrorIs(t, err, appclient.ErrReadStale)
		require.False(t, core.IsDatabaseFailure(err))
	}
}

func TestSavedOrderReceiptSQLDoesNotRecordRefusal(t *testing.T) {
	t.Parallel()
	transport := &r36FailureTransport{status: http.StatusForbidden, code: mediaForbidden, marked: true}
	b := r36FailureBot(transport)
	// No database is attached: the marked denial must return before any refusal write.
	text, found, err := b.savedOrderReceipt(t.Context(), incoming{owner: "alice"}, 36, interaction.SavedPlan{
		OrderCommand: &orders.Command{Name: "pay", EventID: "event", OrderID: "order"},
	}, r36Source())
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Empty(t, text)
	require.False(t, found)
	require.Equal(t, 1, transport.calls)
}

func TestSavedArchiveSQLDoesNotTerminalizePlan(t *testing.T) {
	t.Parallel()
	db := foodPendingDatabase(t)
	for index, marked := range []bool{true, false} {
		var archiveCalls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/v1/me/history/generation":
				_, _ = w.Write([]byte(`{"generation":0}`))
			case "/internal/history/archive/derived":
				archiveCalls.Add(1)
				if marked {
					w.Header().Set(core.DatabaseFailureHeader, "1")
				}
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"code":"history_stale"}`))
			default:
				_, _ = w.Write([]byte(`{"ok":true}`))
			}
		}))
		t.Cleanup(server.Close)
		b := &Bot{DB: db, API: appclient.Client{Base: server.URL, SandboxToken: (identity.Signer{}).Token}}
		b.Host = appclient.Host{Base: server.URL, UserToken: b.API.UserToken}
		plan := interaction.SavedPlan{
			FormatVersion: interaction.CurrentFormatVersion, Kind: interaction.DerivedPlan, State: interaction.Ready,
			Plan: agent.Plan{Text: "reply"}, PassAuthority: &interaction.PlanAuthority{
				Reads: []interaction.PassContextDependency{}, ReadAuthorities: []readsource.Authority{},
			},
		}
		id := int64(360 + index)
		_, err := (interaction.Store{DB: db}).SaveWinner(t.Context(), "alice", id, plan)
		require.NoError(t, err)
		err = b.archiveAssistantReply(t.Context(), "alice", id, "reply")
		server.Close()
		require.EqualValues(t, 1, archiveCalls.Load())
		saved, loadErr := (interaction.Store{DB: db}).Load(t.Context(), "alice", id)
		require.NoError(t, loadErr)
		if marked {
			require.ErrorIs(t, err, core.ErrDatabase)
			require.Empty(t, saved.TerminalReason)
		} else {
			require.ErrorIs(t, err, errHistoryPlanTerminal)
			require.Equal(t, interaction.HistoryDeleted, saved.TerminalReason)
		}
	}
}
