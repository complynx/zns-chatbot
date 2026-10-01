package bot

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type opaqueUpdateError struct{}

func (*opaqueUpdateError) Error() string { panic("private error text must not be read") }

func TestUpdateFailureTypedProvenanceAndSafeCodes(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		err      error
		code     string
		provider int
	}{
		{err: errors.Join(core.ErrDatabase, context.Canceled, &core.ProblemError{Status: 403, Code: "private-code"}), code: "database_failure"},
		{err: core.DatabaseFailure(&core.ProblemError{Status: 409, Code: "stale_version"}), code: "database_failure"},
		{err: errors.Join(core.ErrDatabaseSerialization, &telegram.APIError{Code: 429}), code: "database_serialization"},
		{err: errors.Join(context.Canceled, &opaqueUpdateError{}), code: "canceled"},
		{err: context.DeadlineExceeded, code: "timeout"},
		{err: errHistoryPlanTerminal, code: "terminal_plan"},
		{err: errPassPlanTerminal, code: "terminal_plan"},
		{err: appclient.ErrReadStale, code: "stale_reference"},
		{err: &core.ProblemError{Status: 409, Code: "history_stale"}, code: "stale_reference"},
		{err: identity.ErrZitadelUserInactive, code: "domain_denied"},
		{err: &core.ProblemError{Status: 403, Code: "private-denial"}, code: "domain_denied"},
		{err: &core.ProblemError{Status: 409, Code: "private-rejection"}, code: "domain_rejected"},
		{err: errCreditCutoverLegacyLimit, code: "credit_configuration"},
		{err: &telegram.ControlError{Reason: "private-reason"}, code: "provider_deferred"},
		{err: &telegram.APIError{Code: 401, Description: "private-description"}, code: "provider_denied", provider: 401},
		{err: &telegram.APIError{Code: 403, Description: "private-description"}, code: "provider_denied", provider: 403},
		{err: &telegram.APIError{Code: 429, Description: "private-description"}, code: "provider_rate_limited", provider: 429},
		{err: &telegram.APIError{Code: 503, Description: "private-description"}, code: "provider_failure", provider: 503},
		{err: &telegram.APIError{Code: 100, Description: "private-description"}, code: "provider_failure", provider: 100},
		{err: &telegram.APIError{Code: 599, Description: "private-description"}, code: "provider_failure", provider: 599},
		{err: &telegram.APIError{Code: 99, Description: "private-description"}, code: "operation_failed"},
		{err: &telegram.APIError{Code: 600, Description: "private-description"}, code: "operation_failed"},
		{err: &telegram.APIError{Code: 700, Description: "private-description"}, code: "operation_failed"},
		{err: (*telegram.APIError)(nil), code: "operation_failed"},
		{err: &opaqueUpdateError{}, code: "operation_failed"},
	} {
		require.Equal(t, updateFailure{code: item.code, providerCode: item.provider}, classifyUpdateFailure(item.err))
		var logs bytes.Buffer
		b := Bot{Logger: observability.NewLogger(&logs, observability.LogConfig{})}
		b.observeUpdateFailure(t.Context(), updateFailureHandler, item.err)
		text := logs.String()
		require.Contains(t, text, `"code":"`+item.code+`"`)
		require.Contains(t, text, `"phase":"handler"`)
		require.Contains(t, text, `"transport_status":0`)
		require.Contains(t, text, `"provider_code":`+strconv.Itoa(item.provider))
		require.Contains(t, text, `"retryability":"unknown"`)
		for _, private := range []string{"private", "update_id", "chat_id", "owner", "error\""} {
			require.NotContains(t, text, private)
		}
	}
}

type updateFailureObserver struct {
	starts    int
	finished  error
	operation string
}

func (o *updateFailureObserver) Start(ctx context.Context, operation string) (context.Context, func(error)) {
	o.starts++
	o.operation = operation
	return ctx, func(err error) { o.finished = err }
}

func TestUpdateFailureHandlePreservesHandlerReturnAndCounters(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	observer := &updateFailureObserver{}
	b := Bot{Observer: observer, Logger: observability.NewLogger(&logs, observability.LogConfig{}),
		API: appclient.Client{Exchange: &authExchange{}, Links: authLinks{err: core.ErrDatabase}}}
	update := telegram.Update{
		ID: 9,
		Message: &telegram.Message{
			Text: "private-message",
			From: telegram.User{ID: 101},
			Chat: telegram.Chat{ID: 101, Type: "private"},
		},
	}
	err := b.Handle(t.Context(), update)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Same(t, err, observer.finished, "finish and caller receive the same unchanged handler error")
	require.Equal(t, 1, observer.starts)
	require.Equal(t, "telegram.update", observer.operation)
	require.Contains(t, logs.String(), `"phase":"handler"`)
	require.Contains(t, logs.String(), `"code":"database_failure"`)
	require.NotContains(t, logs.String(), "private")
	b.API.Links = authLinks{err: identity.ErrZitadelIdentity}
	logs.Reset()
	require.NoError(t, b.Handle(t.Context(), update), "existing silent admission denial remains unchanged")
	require.Equal(t, 2, observer.starts)
	require.NoError(t, observer.finished)
	require.NotContains(t, logs.String(), "telegram update failed")
}

func TestUpdateFailureIngressRemainsBeforeObserverStart(t *testing.T) {
	t.Parallel()
	db, err := pgxpool.New(t.Context(), "postgres://synthetic:synthetic@127.0.0.1:1/synthetic?sslmode=disable")
	require.NoError(t, err)
	db.Close()
	var logs bytes.Buffer
	observer := &updateFailureObserver{}
	b := Bot{
		DB:       db,
		Delivery: delivery.Settings{BotID: 1},
		Observer: observer,
		Logger:   observability.NewLogger(&logs, observability.LogConfig{}),
	}
	update := telegram.Update{
		ID: 9,
		Message: &telegram.Message{
			Text: "private-message",
			From: telegram.User{ID: 101},
			Chat: telegram.Chat{ID: 101, Type: "private"},
		},
	}
	err = b.Handle(t.Context(), update)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Zero(t, observer.starts, "ingress failure must not enter handler or alter existing operation counters")
	require.Contains(t, logs.String(), `"phase":"ingress"`)
	require.Contains(t, logs.String(), `"code":"database_failure"`)
	require.NotContains(t, logs.String(), "private")
	logs.Reset()
	b.observeUpdateFailure(t.Context(), updateFailurePhase(200), &opaqueUpdateError{})
	require.Contains(t, logs.String(), `"phase":"unknown"`)
	logs.Reset()
	b.observeUpdateFailure(t.Context(), updateFailureHandler, nil)
	require.Empty(t, logs.String())
}

func TestUpdateFailureHandleUsesActualRuntime(t *testing.T) {
	t.Parallel()
	for _, item := range []struct {
		name, code, result string
		err                error
	}{
		{name: "database", err: core.ErrDatabase, code: "database_failure", result: "error"},
		{name: "cancellation", err: context.Canceled, code: "canceled", result: "canceled"},
		{name: "deadline", err: context.DeadlineExceeded, code: "timeout", result: "timeout"},
	} {
		t.Run(item.name, func(t *testing.T) {
			t.Parallel()
			runtime, err := observability.New(t.Context(), observability.Config{})
			require.NoError(t, err)
			var logs bytes.Buffer
			b := Bot{Observer: runtime, Logger: observability.NewLogger(&logs, observability.LogConfig{}),
				API: appclient.Client{Exchange: &authExchange{}, Links: authLinks{err: item.err}}}
			span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
			ctx := trace.ContextWithSpanContext(t.Context(), span)
			update := telegram.Update{
				ID: 9,
				Message: &telegram.Message{
					Text: "private-message",
					From: telegram.User{ID: 101},
					Chat: telegram.Chat{ID: 101, Type: "private"},
				},
			}
			require.ErrorIs(t, b.Handle(ctx, update), item.err)
			require.Contains(t, logs.String(), `"operation":"telegram.update"`)
			require.Contains(t, logs.String(), `"phase":"handler"`)
			require.Contains(t, logs.String(), `"code":"`+item.code+`"`)
			require.Contains(t, logs.String(), `"retryability":"unknown"`)
			require.Contains(t, logs.String(), `"transport_status":0`)
			require.Contains(t, logs.String(), `"trace_id":"`+span.TraceID().String()+`"`)
			require.NotContains(t, logs.String(), "private-")
			metrics := httptest.NewRecorder()
			runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			require.Contains(
				t,
				metrics.Body.String(),
				`zns_operations_total{operation="telegram.update",result="`+item.result+`"} 1`,
			)
			b.API.Links = authLinks{err: identity.ErrZitadelIdentity}
			logs.Reset()
			require.NoError(t, b.Handle(ctx, update))
			require.NotContains(t, logs.String(), "telegram update failed")
			metrics = httptest.NewRecorder()
			runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			require.Contains(
				t,
				metrics.Body.String(),
				`zns_operations_total{operation="telegram.update",result="ok"} 1`,
			)
			b.Observer = nil
			b.API.Links = authLinks{err: core.ErrDatabase}
			logs.Reset()
			require.ErrorIs(t, b.Handle(ctx, update), core.ErrDatabase)
			require.Empty(t, logs.String(), "the existing nil-observer handler branch is unchanged")
		})
	}
}

func TestUpdateFailureIngressUsesActualRuntimeWithoutStartingOperation(t *testing.T) {
	t.Parallel()
	db, err := pgxpool.New(t.Context(), "postgres://synthetic:synthetic@127.0.0.1:1/synthetic?sslmode=disable")
	require.NoError(t, err)
	db.Close()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	var logs bytes.Buffer
	b := Bot{DB: db, Delivery: delivery.Settings{BotID: 1}, Observer: runtime,
		Logger: observability.NewLogger(&logs, observability.LogConfig{})}
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}})
	ctx := trace.ContextWithSpanContext(t.Context(), span)
	update := telegram.Update{
		ID: 9,
		Message: &telegram.Message{
			Text: "private-message",
			From: telegram.User{ID: 101},
			Chat: telegram.Chat{ID: 101, Type: "private"},
		},
	}
	require.ErrorIs(t, b.Handle(ctx, update), core.ErrDatabase)
	require.Contains(t, logs.String(), `"phase":"ingress"`)
	require.Contains(t, logs.String(), `"code":"database_failure"`)
	require.Contains(t, logs.String(), `"trace_id":"`+span.TraceID().String()+`"`)
	require.NotContains(t, logs.String(), "private-")
	metrics := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.NotContains(t, metrics.Body.String(), `operation="telegram.update"`)
}
