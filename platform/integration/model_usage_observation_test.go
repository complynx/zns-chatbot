package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestModelUsageObservationDurableReplayWindowAndBound(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := credits.Service{DB: db}
	reserve := func() string {
		id := uuid.NewString()
		require.NoError(
			t,
			service.Reserve(
				t.Context(),
				credits.Attempt{
					ID:        id,
					Scope:     credits.Scope{Actor: "private-actor", Payer: "private-payer", Key: id},
					Operation: "private-operation",
					Provider:  "private-provider",
					Model:     "private-model",
				},
			),
		)
		return id
	}
	known := reserve()
	require.NoError(t, service.Dispatch(t.Context(), known))
	input, cached, output := int64(12), int64(3), int64(0)
	value := credits.Settlement{
		Usage: credits.Usage{
			Basis:     "reported",
			Input:     &input,
			Cached:    &cached,
			Output:    &output,
			RequestID: "private-receipt",
		},
		CostBasis: "unknown",
	}
	require.NoError(t, service.Settle(t.Context(), known, value))
	durableKnown := func() string {
		var raw string
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT usage::text FROM credits.attempts WHERE id=$1`, known).Scan(&raw),
		)
		return raw
	}
	stored := durableKnown()
	require.JSONEq(
		t,
		`{"usage":{"basis":"reported","input_tokens":12,"cached_input_tokens":3,"output_tokens":0,"request_id":"private-receipt"},"cost_basis":"unknown"}`,
		stored,
		"the observer must decode the actual JSON stored by Reserve/Dispatch/Settle",
	)
	require.NoError(
		t,
		service.Settle(t.Context(), known, value),
		"durable settlement replay must not duplicate sampled receipt",
	)
	require.JSONEq(t, stored, durableKnown(), "identical settlement replay preserves its durable receipt")
	var knownRows int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM credits.attempts WHERE id=$1 AND state='settled'`, known).
			Scan(&knownRows),
	)
	require.Equal(t, int64(1), knownRows)
	unknown := reserve()
	require.NoError(t, service.Dispatch(t.Context(), unknown))
	require.NoError(
		t,
		service.Settle(
			t.Context(),
			unknown,
			credits.Settlement{Usage: credits.Usage{Basis: "unknown"}, CostBasis: "unknown"},
		),
	)
	dispatched := reserve()
	require.NoError(t, service.Dispatch(t.Context(), dispatched))
	reserve()
	notSent := reserve()
	require.NoError(t, service.NotSent(t.Context(), notSent))
	old, future := reserve(), reserve()
	_, err := db.Exec(
		t.Context(),
		`UPDATE credits.attempts SET created_at=CASE WHEN id=$1 THEN statement_timestamp()-interval '2 days' ELSE statement_timestamp()+interval '2 days' END WHERE id IN ($1,$2)`,
		old,
		future,
	)
	require.NoError(t, err)
	ledger := func() string {
		var text string
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(a) ORDER BY id)::text FROM credits.attempts a`).
				Scan(&text),
		)
		return text
	}
	before := ledger()
	item, err := service.ObserveUsage(t.Context())
	require.NoError(t, err)
	require.Equal(t, [4]int64{1, 1, 2, 1}, item.States)
	require.Equal(t, [4]int64{1, 0, 1, 0}, item.Bases)
	require.False(t, item.Truncated)
	require.Equal(t, credits.UsageTokenObservation{Sum: 12, KnownReceipts: 1, UnknownReceipts: 1}, item.Tokens[0])
	require.Equal(t, int64(3), item.Tokens[1].Sum)
	require.Equal(t, int64(2), item.Tokens[2].UnknownReceipts)
	require.Equal(t, credits.UsageTokenObservation{KnownReceipts: 1, UnknownReceipts: 1}, item.Tokens[3])
	again, err := (credits.Service{DB: db}).ObserveUsage(t.Context())
	require.NoError(t, err)
	require.Equal(t, item, again)
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	require.NoError(t, runtime.RegisterModelUsage(service))
	require.Error(t, runtime.RegisterModelUsage(service))
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, 200, response.Code)
	for _, private := range []string{"private-", "payer", "actor", "model=", "request_id"} {
		require.NotContains(t, response.Body.String(), private)
	}
	require.JSONEq(t, before, ledger(), "scrapes must preserve exact durable accounting and timestamps")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.ObserveUsage(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO credits.attempts(id,operation_key,actor,payer,operation,provider,model,state)
 SELECT md5('observation-'||n)::uuid,'sample','sample','sample','sample','sample','sample','reserved' FROM generate_series(1,1025) n`,
	)
	require.NoError(t, err)
	item, err = service.ObserveUsage(t.Context())
	require.NoError(t, err)
	require.True(t, item.Truncated)
	require.Equal(t, [4]int64{1024, 0, 0, 0}, item.States, "only newest bounded sample contributes")
	require.Equal(t, [7]credits.UsageTokenObservation{}, item.Tokens)
}
