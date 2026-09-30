package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/assistantsource"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestSourceObservationPreservesMaintenanceStartupFailure(t *testing.T) {
	t.Parallel()
	for _, observed := range []bool{false, true} {
		services := unreachableServices(t)
		if !observed {
			services.Knowledge = knowledge.Service{}
		}
		runtime := sourceObservationRuntime(t)
		stop, err := startProductMaintenance(t.Context(), services.Core.DB, slog.New(slog.DiscardHandler),
			config.Config{}, services, runtime, func(error) {})
		require.Nil(t, stop)
		require.ErrorIs(t, err, core.ErrDatabase, "the existing maintenance SQL failure remains authoritative")
		text := sourceObservationScrape(t, runtime)
		if observed {
			require.Contains(t, text, "zns_assistant_source_snapshot_available 0")
		} else {
			require.NotContains(t, text, "zns_assistant_source_")
		}
		require.NotContains(t, text, "private-")
	}
}

func TestSourceObservationUsesSuppliedKnowledgeAtMaintenanceStartup(t *testing.T) {
	t.Parallel()
	workersDB, sourceDB := runtimeServerDatabase(t), runtimeServerDatabase(t)
	require.NoError(t, store.Migrate(t.Context(), workersDB))
	require.NoError(t, store.Migrate(t.Context(), sourceDB))
	services := appservices.NewServices(workersDB, appservices.Options{Delivery: delivery.Settings{
		BotID: 1, BotInterval: time.Millisecond, ChatInterval: time.Millisecond, Fallback: time.Second,
	}})
	services.Knowledge = knowledge.Service{DB: sourceDB}
	identity := assistantsource.Digest([]byte("private-source-identity"))
	digest := assistantsource.Digest([]byte("private-source-body"))
	require.NoError(t, services.Knowledge.ConfigureSource(t.Context(), knowledge.AssistantAbout, identity))
	require.NoError(t, services.Knowledge.ReplaceSource(t.Context(), knowledge.AssistantAbout,
		identity, digest, []string{"private-source-body"}))
	before := sourceObservationLedger(t, sourceDB)
	runtime := sourceObservationRuntime(t)
	cfg := config.Config{Orders: config.Orders{ReminderAfter: time.Hour}}
	stop, err := startProductMaintenance(t.Context(), workersDB, slog.New(slog.DiscardHandler),
		cfg, services, runtime, func(error) {})
	require.NoError(t, err)
	t.Cleanup(stop)
	text := sourceObservationScrape(t, runtime)
	require.Contains(t, text, `zns_assistant_source_state{source="assistant_about",state="ready"} 1`)
	require.Contains(t, text, `zns_assistant_source_version{source="assistant_about"} 1`)
	require.Contains(t, text, `zns_assistant_source_present{source="assistant_qa"} 0`)
	require.JSONEq(t, before, sourceObservationLedger(t, sourceDB), "scrapes do not reconfigure or publish sources")
	for _, private := range []string{identity, digest, "private-source", "identity=", "url="} {
		require.NotContains(t, text, private)
	}
	require.Error(t, runtime.RegisterAssistantSources(services.Knowledge), "one collector per owner runtime")
	stop()
	// Disabled refresh still has its existing dormant registry in the worker DB.
	services.Knowledge = knowledge.Service{}
	absent := sourceObservationRuntime(t)
	stop, err = startProductMaintenance(t.Context(), workersDB, slog.New(slog.DiscardHandler),
		cfg, services, absent, func(error) {})
	require.NoError(t, err, "an absent optional observation service does not add a startup rejection")
	stop()
	require.NotContains(t, sourceObservationScrape(t, absent), "zns_assistant_source_")
	sourceDB.Close()
	text = sourceObservationScrape(t, runtime)
	require.Contains(t, text, "zns_assistant_source_snapshot_available 0")
	require.NotContains(t, text, "zns_assistant_source_state{")
	require.NotContains(t, text, "zns_assistant_source_version{")
}

func sourceObservationRuntime(t *testing.T) *observability.Runtime {
	t.Helper()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Shutdown(context.Background())) })
	return runtime
}

func sourceObservationScrape(t *testing.T, runtime *observability.Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}

func sourceObservationLedger(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var result string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'sources',(SELECT jsonb_agg(to_jsonb(s) ORDER BY slot) FROM core.assistant_sources s),
 'documents',(SELECT jsonb_agg(to_jsonb(d) ORDER BY slot,item_key) FROM core.assistant_source_documents d),
 'versions',(SELECT jsonb_agg(to_jsonb(v) ORDER BY slot,version) FROM core.assistant_source_versions v))::text`).Scan(&result))
	return result
}
