package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestBotModelUsageAbsentWithoutDurableAccounting(t *testing.T) {
	t.Parallel()
	runtime := modelUsageRuntime(t)
	cfg := config.Config{Model: config.Model{Provider: "scripted"}}
	_, err := botModel(cfg, runtime)
	require.NoError(t, err)
	assert.NotContains(t, modelUsageScrape(t, runtime), "zns_model_usage_sample_")
	_, err = botModel(cfg, runtime, credits.Service{})
	require.Error(t, err, "a supplied service must have its actual accounting DB")
	assert.NotContains(t, modelUsageScrape(t, runtime), "zns_model_usage_sample_")
}

func TestBotModelUsageDurableAccountingBinding(t *testing.T) {
	t.Parallel()
	db := runtimeServerDatabase(t)
	require.NoError(t, store.Migrate(t.Context(), db))
	runtime := modelUsageRuntime(t)
	service := credits.Service{DB: db}
	cfg := config.Config{Model: config.Model{Provider: openAIProvider, OpenAIKey: "synthetic-unused"}}
	model, err := botModel(cfg, runtime, service)
	require.NoError(t, err)
	observed, ok := model.(observedModel)
	require.True(t, ok)
	provider, ok := observed.next.(agent.OpenAI)
	require.True(t, ok)
	assert.Equal(t, service, provider.Accounting, "observe the same recorder without replacing accounting")
	empty := modelUsageScrape(t, runtime)
	require.Contains(t, empty, "zns_model_usage_sample_available 1")
	require.Contains(t, empty, `zns_model_usage_sample_attempts{state="settled"} 0`)
	_, err = botModel(cfg, runtime, service)
	require.Error(t, err, "one collector per registry; repeated construction must not duplicate series")
	known := reserveModelUsageAttempt(t, service)
	require.NoError(t, service.Dispatch(t.Context(), known))
	input, output := int64(12), int64(0)
	settlement := credits.Settlement{
		Usage: credits.Usage{Basis: "reported", Input: &input, Output: &output}, CostBasis: "unknown",
	}
	require.NoError(t, service.Settle(t.Context(), known, settlement))
	require.NoError(t, service.Settle(t.Context(), known, settlement))
	unknown := reserveModelUsageAttempt(t, service)
	require.NoError(t, service.Dispatch(t.Context(), unknown))
	require.NoError(t, service.Settle(t.Context(), unknown,
		credits.Settlement{Usage: credits.Usage{Basis: "unknown"}, CostBasis: "unknown"}))
	before := modelUsageLedger(t, db)
	text := modelUsageScrape(t, runtime)
	require.Contains(t, text, `zns_model_usage_sample_attempts{state="settled"} 2`)
	require.Contains(t, text, `zns_model_usage_sample_tokens{kind="input"} 12`)
	require.Contains(t, text, `zns_model_usage_sample_tokens{kind="output"} 0`)
	require.Contains(t, text, `zns_model_usage_sample_known_receipts{kind="output"} 1`)
	require.Contains(t, text, `zns_model_usage_sample_unknown_receipts{kind="output"} 1`)
	require.JSONEq(t, before, modelUsageLedger(t, db), "the actual runtime scrape never updates accounting")
	for _, private := range []string{"private-", known, unknown, "actor=", "payer=", "model="} {
		assert.NotContains(t, text, private)
	}
	db.Close()
	unavailable := modelUsageScrape(t, runtime)
	require.Contains(t, unavailable, "zns_model_usage_sample_available 0")
	assert.NotContains(t, unavailable, "zns_model_usage_sample_tokens{")
	assert.NotContains(t, unavailable, "zns_model_usage_sample_attempts{")
}

func modelUsageRuntime(t *testing.T) *observability.Runtime {
	t.Helper()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, runtime.Shutdown(context.Background())) })
	return runtime
}

func modelUsageScrape(t *testing.T, runtime *observability.Runtime) string {
	t.Helper()
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}

func reserveModelUsageAttempt(t *testing.T, service credits.Service) string {
	t.Helper()
	id := uuid.NewString()
	require.NoError(t, service.Reserve(t.Context(), credits.Attempt{
		ID: id, Scope: credits.Scope{Actor: "private-actor", Payer: "private-payer", Key: id},
		Operation: "private-operation", Provider: "private-provider", Model: "private-model",
	}))
	return id
}

func modelUsageLedger(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var raw string
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM credits.attempts a`).Scan(&raw))
	return raw
}
