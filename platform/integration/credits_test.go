package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsDispatchAndSettlementFences(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := credits.Service{DB: db}
	attempt := credits.Attempt{
		ID:        uuid.NewString(),
		Scope:     credits.Scope{Actor: "actor", Payer: "payer", Key: "update:1"},
		Operation: "test.plan",
		Provider:  "synthetic",
		Model:     "test-model",
	}
	require.NoError(t, service.Reserve(t.Context(), attempt))
	require.NoError(t, service.Reserve(t.Context(), attempt))
	wrong := attempt
	wrong.Scope.Payer = "other"
	require.ErrorIs(t, service.Reserve(t.Context(), wrong), credits.ErrConflict)
	var wg sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		wg.Go(func() { outcomes <- service.Dispatch(t.Context(), attempt.ID) })
	}
	wg.Wait()
	close(outcomes)
	success := 0
	for err := range outcomes {
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, credits.ErrConflict)
		}
	}
	require.Equal(t, 1, success)
	value := credits.Settlement{Usage: credits.Usage{Basis: "unknown"}, CostBasis: "unknown"}
	require.NoError(t, service.Settle(t.Context(), attempt.ID, value))
	require.NoError(t, service.Settle(t.Context(), attempt.ID, value))
	value.Usage.RequestID = "different"
	require.ErrorIs(t, service.Settle(t.Context(), attempt.ID, value), credits.ErrConflict)
	var amount *int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT cost_nano_usd FROM credits.attempts WHERE id=$1`, attempt.ID).Scan(&amount),
	)
	require.Nil(t, amount)
}

func TestCreditsProviderFailureStillRecordsUsage(t *testing.T) {
	t.Parallel()
	db := database(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(
			[]byte(
				`{"id":"synthetic-request","model":"synthetic-model","status":"incomplete","usage":{"input_tokens":12,"output_tokens":7,"input_tokens_details":{"cached_tokens":3},"output_tokens_details":{"reasoning_tokens":7}},"output":[]}`,
			),
		)
	}))
	defer provider.Close()
	model := agent.OpenAI{
		Key:        "synthetic",
		BaseURL:    provider.URL,
		HTTP:       provider.Client(),
		Accounting: credits.Service{DB: db},
	}
	ctx := credits.WithScope(
		t.Context(),
		credits.Scope{Actor: "campaign-admin", Payer: "campaign-admin", Key: "broadcast:1:2"},
	)
	_, err := model.InformalName(ctx, map[string]any{"first_name": "private-name-not-in-ledger"})
	require.Error(t, err)
	var payer, state, usage, costBasis string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT payer,state,usage::text,cost_basis FROM credits.attempts`).
			Scan(&payer, &state, &usage, &costBasis),
	)
	require.Equal(t, "campaign-admin", payer)
	require.Equal(t, "settled", state)
	require.Equal(t, "unknown", costBasis)
	require.Contains(t, usage, `"reasoning_tokens": 7`)
	require.Contains(t, usage, `"basis": "reported"`)
	require.NotContains(t, usage, "private-name")
	require.NotContains(t, usage, "output\"")
}

func TestCreditsImmutablePriceRevision(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := credits.Service{DB: db}
	revision := credits.PriceRevision{
		Provider: "synthetic",
		Model:    "model",
		Source:   "fixture",
		Price: credits.Price{
			Currency:          "USD",
			Version:           "v1",
			ConversionVersion: "fx-v1",
			Input:             "0.1",
			Cached:            "0.01",
			CacheWrite:        "0",
			Output:            "0.2",
			Conversion:        "1/2",
		},
	}
	require.NoError(t, service.RegisterPrice(t.Context(), revision))
	require.NoError(t, service.RegisterPrice(t.Context(), revision))
	revision.Price.Input = "0.3"
	require.ErrorIs(t, service.RegisterPrice(t.Context(), revision), credits.ErrConflict)
	input, cached, output := int64(2), int64(1), int64(1)
	quote, err := service.Quote(
		t.Context(),
		"synthetic",
		"model",
		"v1",
		credits.Usage{Basis: "reported", Input: &input, Cached: &cached, Output: &output},
	)
	require.NoError(t, err)
	require.Equal(t, int64(155000000), *quote.CostNanoUSD)
	require.Equal(t, "estimated", quote.CostBasis)
	require.Equal(t, "31/100", quote.OriginalAmount)
	require.Equal(t, "USD", quote.OriginalCurrency)
	require.NoError(t, service.SelectPrice(t.Context(), "synthetic", "model", "v1"))
	attempt := credits.Attempt{
		ID:        uuid.NewString(),
		Scope:     credits.Scope{Actor: "owner", Payer: "owner", Key: "priced"},
		Provider:  "synthetic",
		Model:     "model",
		Operation: "test.plan",
	}
	require.NoError(t, service.Reserve(t.Context(), attempt))
	revision.Price.Version = "v2"
	require.NoError(t, service.RegisterPrice(t.Context(), revision))
	require.NoError(t, service.SelectPrice(t.Context(), "synthetic", "model", "v2"))
	require.NoError(t, service.Dispatch(t.Context(), attempt.ID))
	quote.Usage.Model = "model"
	measurement := credits.Settlement{Usage: quote.Usage, CostBasis: "unknown"}
	require.NoError(t, service.Settle(t.Context(), attempt.ID, measurement))
	require.NoError(t, service.Settle(t.Context(), attempt.ID, measurement))
	var amount int64
	var version string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT cost_nano_usd,price_version FROM credits.attempts WHERE id=$1`, attempt.ID).
			Scan(&amount, &version),
	)
	require.Equal(t, "v1", version)
	require.Equal(t, int64(155000000), amount)
	_, err = service.Quote(t.Context(), "wrong-provider", "model", "v1", quote.Usage)
	require.Error(t, err)
}

func TestCreditsFinishAfterCancellationAndCrashRemainsUnknown(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := credits.Service{DB: db}
	ctx, cancel := context.WithCancel(
		credits.WithScope(
			t.Context(),
			credits.Scope{Actor: "unlimited-admin", Payer: "unlimited-admin", Key: "test:1"},
		),
	)
	call, err := credits.Begin(ctx, service, "test.plan", "synthetic", "test-model")
	require.NoError(t, err)
	cancel()
	require.NoError(t, call.Finish(ctx))
	_, err = credits.Begin(t.Context(), service, "test.crash", "synthetic", "test-model")
	require.NoError(t, err)
	var settled, unresolved int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FILTER(WHERE state='settled'),count(*) FILTER(WHERE state='dispatched' AND cost_nano_usd IS NULL) FROM credits.attempts`).
			Scan(&settled, &unresolved),
	)
	require.Equal(t, 1, settled)
	require.Equal(t, 1, unresolved)
}
