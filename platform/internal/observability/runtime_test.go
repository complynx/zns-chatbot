package observability_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestDisabledMetricsAndCardinality(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	for index := range 100 {
		_, finish := runtime.Start(t.Context(), "/users/"+strconv.Itoa(index)+"?secret=yes")
		finish(errors.New("password-secret"))
		handler := runtime.HTTPHandler("server", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		handler.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequest("CUSTOM"+strconv.Itoa(index), "/private/"+strconv.Itoa(index), nil),
		)
	}
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := response.Body.String()
	assert.Contains(t, text, `zns_operations_total{operation="unknown",result="error"} 100`)
	assert.Contains(t, text, `zns_operations_total{operation="server",result="ok"} 100`)
	assert.NotContains(t, text, "secret")
	assert.NotContains(t, text, "CUSTOM")
	assert.NotContains(t, text, "/private/")
	families, err := runtime.Registry.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == "zns_operation_failures_total" {
			assert.Len(t, family.GetMetric(), 1)
		} else {
			assert.Len(t, family.GetMetric(), 2)
		}
	}
	require.NoError(t, runtime.Shutdown(t.Context()))
}

type spanSink struct {
	mu    sync.Mutex
	spans []*tracepb.Span
}

func (s *spanSink) serve(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var request collector.ExportTraceServiceRequest
	if err = proto.Unmarshal(body, &request); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, resource := range request.GetResourceSpans() {
		for _, scope := range resource.GetScopeSpans() {
			s.spans = append(s.spans, scope.GetSpans()...)
		}
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

func TestOTLPDeliveryParentCorrelationAndPrivacy(t *testing.T) {
	t.Parallel()
	var sink spanSink
	collectorServer := httptest.NewServer(http.HandlerFunc(sink.serve))
	defer collectorServer.Close()
	runtime, err := observability.New(
		t.Context(),
		observability.Config{Enabled: true, Endpoint: collectorServer.URL, SampleRatio: 1},
	)
	require.NoError(t, err)
	var output bytes.Buffer
	logger := observability.NewLogger(&output, observability.LogConfig{})
	var receivedBaggage string
	server := httptest.NewServer(
		runtime.HTTPHandler("server", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			receivedBaggage = r.Header.Get("Baggage")
			ctx, finish := runtime.Start(r.Context(), "db")
			logger.InfoContext(ctx, "query complete")
			finish(errors.New("postgres://user:secret@database/private"))
			w.WriteHeader(http.StatusNoContent)
		})),
	)
	defer server.Close()
	ctx, finish := runtime.Start(t.Context(), "telegram.update")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/user/secret?token=private", nil)
	require.NoError(t, err)
	request.Header.Set("Baggage", "user=private")
	client := &http.Client{Transport: runtime.HTTPClient("api", http.DefaultTransport), Timeout: time.Second}
	response, err := client.Do(request)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, http.StatusNoContent, response.StatusCode)
	assert.Empty(t, receivedBaggage)
	assert.Equal(t, "user=private", request.Header.Get("Baggage"))
	assert.Empty(t, request.Header.Get("Traceparent"))
	finish(nil)
	shutdownCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, runtime.Shutdown(shutdownCtx))
	sink.mu.Lock()
	defer sink.mu.Unlock()
	require.Len(t, sink.spans, 4)
	byName := map[string]*tracepb.Span{}
	for _, span := range sink.spans {
		byName[span.GetName()] = span
		assert.NotContains(t, span.String(), "secret")
		if span.GetName() == "db" {
			assert.Len(t, span.GetAttributes(), 5)
		} else {
			assert.Empty(t, span.GetAttributes())
		}
	}
	assert.Equal(t, byName["telegram.update"].GetSpanId(), byName["api"].GetParentSpanId())
	assert.Equal(t, byName["api"].GetSpanId(), byName["server"].GetParentSpanId())
	assert.Equal(t, byName["server"].GetSpanId(), byName["db"].GetParentSpanId())
	assert.Contains(t, output.String(), `"trace_id"`)
	assert.Contains(t, output.String(), `"span_id"`)
	assert.Contains(t, output.String(), hex.EncodeToString(byName["db"].GetTraceId()))
	assert.Contains(t, output.String(), hex.EncodeToString(byName["db"].GetSpanId()))
	assert.NotContains(t, output.String(), "secret")
}

type completionFailureCase struct {
	err      error
	code     string
	coarse   string
	provider int
}

type serializationOnlyError struct{}

func (serializationOnlyError) Error() string        { return "private-serialization-secret" }
func (serializationOnlyError) Is(target error) bool { return target == core.ErrDatabaseSerialization }

type counterfeitAPIError struct{}

func (counterfeitAPIError) Error() string { return "private-counterfeit-secret" }

func (counterfeitAPIError) As(target any) bool {
	api, ok := target.(**telegram.APIError)
	if ok {
		*api = &telegram.APIError{Code: http.StatusTooManyRequests, Description: "private-fabricated-secret"}
	}
	return ok
}

type counterfeitCompletionError struct{ claim string }

type counterfeitSingleUnwrapError struct{}

func (counterfeitSingleUnwrapError) Error() string { return "private-fabricated-secret" }
func (counterfeitSingleUnwrapError) Unwrap() error { return core.ErrDatabase }

type counterfeitMultiUnwrapError struct{}

func (counterfeitMultiUnwrapError) Error() string   { return "private-fabricated-secret" }
func (counterfeitMultiUnwrapError) Unwrap() []error { return []error{core.ErrDatabase} }

func (counterfeitCompletionError) Error() string { return "private-counterfeit-secret" }

func (e counterfeitCompletionError) As(target any) bool {
	switch value := target.(type) {
	case **core.ProblemError:
		if e.claim == "domain" {
			*value = &core.ProblemError{Status: http.StatusForbidden, Code: "private-fabricated-secret"}
			return true
		}
	case **pgconn.PgError:
		if e.claim == "statement" {
			*value = &pgconn.PgError{Code: "42P01", Message: "private-fabricated-secret"}
			return true
		}
	case **pgconn.ConnectError:
		if e.claim == "connection" {
			*value = &pgconn.ConnectError{}
			return true
		}
	case **telegram.ControlError:
		if e.claim == "control" {
			*value = &telegram.ControlError{Reason: "private-fabricated-secret"}
			return true
		}
	case *interface {
		error
		Unwrap() error
	}:
		if e.claim == "single" {
			*value = counterfeitSingleUnwrapError{}
			return true
		}
	case *interface {
		error
		Unwrap() []error
	}:
		if e.claim == "multi" {
			*value = counterfeitMultiUnwrapError{}
			return true
		}
	}
	return false
}

func completionFailureCases(t *testing.T) []completionFailureCase {
	t.Helper()
	require.ErrorIs(t, serializationOnlyError{}, core.ErrDatabaseSerialization)
	require.NotErrorIs(t, serializationOnlyError{}, core.ErrDatabase)
	cases := []completionFailureCase{
		{core.ErrDatabase, "database_failure", "error", 0},
		{core.ErrDatabaseSerialization, "database_serialization", "error", 0},
		{serializationOnlyError{}, "database_serialization", "error", 0},
		{fmt.Errorf("private-wrapper-secret: %w", serializationOnlyError{}), "database_serialization", "error", 0},
		{&pgconn.PgError{Code: "42P01", Message: "private-query-secret"}, "database_failure", "error", 0},
		{errors.Join(core.ErrDatabase, context.Canceled), "database_failure", "canceled", 0},
		{
			errors.Join(
				core.ErrDatabaseSerialization,
				context.DeadlineExceeded,
				&core.ProblemError{
					Status: http.StatusForbidden,
					Code:   "private-owner-secret",
				},
			),
			"database_serialization",
			"timeout",
			0,
		},
		{context.Canceled, "canceled", "canceled", 0},
		{context.DeadlineExceeded, "timeout", "timeout", 0},
		{&core.ProblemError{Status: http.StatusForbidden, Code: "private-owner-secret"}, "domain_denied", "error", 0},
		{&core.ProblemError{Status: http.StatusConflict, Code: "private-owner-secret"}, "domain_rejected", "error", 0},
		{
			&core.ProblemError{Status: http.StatusServiceUnavailable, Code: "private-owner-secret"},
			"operation_failed",
			"error",
			0,
		},
		{
			&telegram.APIError{Code: http.StatusUnauthorized, Description: "private-token-secret"},
			"provider_denied",
			"error",
			401,
		},
		{
			&telegram.APIError{Code: http.StatusTooManyRequests, Description: "private-token-secret"},
			"provider_rate_limited",
			"error",
			429,
		},
		{
			&telegram.APIError{Code: http.StatusServiceUnavailable, Description: "private-token-secret"},
			"provider_failure",
			"error",
			503,
		},
		{&telegram.ControlError{Reason: "private-owner-secret"}, "provider_control_blocked", "error", 0},
		{&telegram.APIError{Code: 99, Description: "private-token-secret"}, "operation_failed", "error", 0},
		{&telegram.APIError{Code: 599, Description: "private-token-secret"}, "provider_failure", "error", 599},
		{&telegram.APIError{Code: 600, Description: "private-token-secret"}, "operation_failed", "error", 0},
		{(*telegram.APIError)(nil), "operation_failed", "error", 0},
		{(*core.ProblemError)(nil), "operation_failed", "error", 0},
		{(*pgconn.PgError)(nil), "operation_failed", "error", 0},
		{(*pgconn.ConnectError)(nil), "operation_failed", "error", 0},
		{(*telegram.ControlError)(nil), "operation_failed", "error", 0},
		{counterfeitAPIError{}, "operation_failed", "error", 0},
		{fmt.Errorf("private-wrapper-secret: %w", counterfeitAPIError{}), "operation_failed", "error", 0},
		{errors.New("https://private-host/secret?token=private-token"), "operation_failed", "error", 0},
	}
	cases = append(cases, databaseCompletionFailureCases(t)...)
	cases = append(cases, databaseProviderCompletionFailureCases(t)...)
	cases = append(cases, joinedProviderCompletionFailureCases(t)...)
	cases = append(cases, counterfeitCompletionFailureCases(t)...)
	return append(cases, controlCompletionFailureCases(t)...)
}

func counterfeitCompletionFailureCases(t *testing.T) []completionFailureCase {
	t.Helper()
	actual := []completionFailureCase{
		{&core.ProblemError{Status: http.StatusConflict, Code: "private-domain-secret"}, "domain_rejected", "error", 0},
		{
			&telegram.APIError{Code: http.StatusUnauthorized, Description: "private-wire-secret"},
			"provider_denied",
			"error",
			401,
		},
		{core.ErrDatabaseSerialization, "database_serialization", "error", 0},
		{core.DatabaseFailure(errors.New("private-sql-secret")), "database_failure", "error", 0},
		{&pgconn.PgError{Code: "42P01", Message: "private-query-secret"}, "database_failure", "error", 0},
		{completionConnectFailure(t, errors.New("private-connection-secret")), "database_failure", "error", 0},
		{&telegram.ControlError{Reason: "private-control-secret"}, "provider_control_blocked", "error", 0},
	}
	var cases []completionFailureCase
	for _, claim := range []string{"domain", "statement", "connection", "control", "single", "multi"} {
		fake := counterfeitCompletionError{claim: claim}
		cases = append(cases, completionFailureCase{fake, "operation_failed", "error", 0},
			completionFailureCase{fmt.Errorf("private-wrapper-secret: %w", fake), "operation_failed", "error", 0})
		for _, sibling := range actual {
			wrapped := fmt.Errorf(
				"private-wrapper-secret: %w",
				errors.Join(errors.New("private-sibling-secret"), sibling.err),
			)
			for _, joined := range []error{errors.Join(fake, wrapped), errors.Join(wrapped, fake)} {
				sibling.err = joined
				cases = append(cases, sibling)
			}
		}
	}
	for _, invalid := range []error{(*pgconn.PgError)(nil), (*pgconn.ConnectError)(nil), (*telegram.ControlError)(nil)} {
		for _, sibling := range actual {
			for _, joined := range []error{errors.Join(invalid, sibling.err), errors.Join(sibling.err, invalid)} {
				cases = append(cases, completionFailureCase{joined, sibling.code, sibling.coarse, sibling.provider})
			}
		}
	}
	return cases
}

func databaseCompletionFailureCases(t *testing.T) []completionFailureCase {
	t.Helper()
	var cases []completionFailureCase
	for _, cancellation := range []error{context.Canceled, context.DeadlineExceeded} {
		coarse := "canceled"
		if errors.Is(cancellation, context.DeadlineExceeded) {
			coarse = "timeout"
		}
		statement := &pgconn.PgError{Code: "42P01", Message: "private-query-secret"}
		connection := completionConnectFailure(t, errors.New("private-connection-secret"))
		cancelledConnection := completionConnectFailure(t, cancellation)
		for _, driver := range []error{statement, connection} {
			cases = append(cases,
				completionFailureCase{errors.Join(driver, cancellation), "database_failure", coarse, 0},
				completionFailureCase{errors.Join(cancellation, driver), "database_failure", coarse, 0},
			)
		}
		cases = append(
			cases,
			completionFailureCase{
				errors.Join(serializationOnlyError{}, cancellation),
				"database_serialization",
				coarse,
				0,
			},
			completionFailureCase{
				errors.Join(cancellation, serializationOnlyError{}),
				"database_serialization",
				coarse,
				0,
			},
			completionFailureCase{errors.Join(cancelledConnection, connection), "database_failure", coarse, 0},
			completionFailureCase{errors.Join(connection, cancelledConnection), "database_failure", coarse, 0},
			completionFailureCase{
				fmt.Errorf("private-outer-secret: %w", errors.Join(cancelledConnection, connection)),
				"database_failure", coarse, 0,
			},
			completionFailureCase{
				fmt.Errorf("private-outer-secret: %w", errors.Join(connection, cancelledConnection)),
				"database_failure", coarse, 0,
			},
			completionFailureCase{connection, "database_failure", "error", 0},
			completionFailureCase{cancelledConnection, coarse, coarse, 0},
			completionFailureCase{errors.Join(cancelledConnection, cancellation), coarse, coarse, 0},
			completionFailureCase{errors.Join(cancellation, cancelledConnection), coarse, coarse, 0},
			completionFailureCase{fmt.Errorf("private-request-secret: %w", cancellation), coarse, coarse, 0},
			completionFailureCase{errors.Join(cancellation, errors.New("private-transport-secret")), coarse, coarse, 0},
		)
	}
	return cases
}

func databaseProviderCompletionFailureCases(t *testing.T) []completionFailureCase {
	t.Helper()
	var cases []completionFailureCase
	for _, provider := range []int{401, 403, 429, 99, 600} {
		bounded := provider
		if provider < 100 || provider > 599 {
			bounded = 0
		}
		api := &telegram.APIError{Code: provider, Description: "private-wire-secret"}
		for _, database := range []error{core.ErrDatabaseSerialization, serializationOnlyError{}, &pgconn.PgError{Code: "42P01", Message: "private-query-secret"}} {
			code := "database_failure"
			if errors.Is(database, core.ErrDatabaseSerialization) {
				code = "database_serialization"
			}
			for _, joined := range []error{errors.Join(api, database), errors.Join(database, api)} {
				cases = append(cases, completionFailureCase{
					err: fmt.Errorf("private-outer-secret: %w", joined), code: code, coarse: "error", provider: bounded,
				})
			}
		}
	}
	return cases
}

func joinedProviderCompletionFailureCases(t *testing.T) []completionFailureCase {
	t.Helper()
	api := &telegram.APIError{Code: 429, Description: "private-wire-secret"}
	var cases []completionFailureCase
	actual := fmt.Errorf("private-wrapper-secret: %w", errors.Join(
		errors.New("private-sibling-secret"),
		&telegram.APIError{Code: http.StatusUnauthorized, Description: "private-wire-secret"},
	))
	for _, joined := range []error{errors.Join(counterfeitAPIError{}, actual), errors.Join(actual, counterfeitAPIError{})} {
		cases = append(cases, completionFailureCase{joined, "provider_denied", "error", http.StatusUnauthorized})
	}
	for _, status := range []int{401, 403, 409} {
		problem := &core.ProblemError{Status: status, Code: "private-domain-secret"}
		code := "domain_denied"
		if status == 409 {
			code = "domain_rejected"
		}
		for _, joined := range []error{errors.Join(api, problem), errors.Join(problem, api)} {
			cases = append(cases, completionFailureCase{
				err: fmt.Errorf("private-outer-secret: %w", joined), code: code, coarse: "error", provider: 429,
			})
		}
	}
	for _, invalid := range []error{&telegram.APIError{Code: 99}, &telegram.APIError{Code: 600}, (*telegram.APIError)(nil)} {
		wrapped := fmt.Errorf("private-invalid-secret: %w", invalid)
		nested := errors.Join(errors.New("private-sibling-secret"), api)
		for _, joined := range []error{errors.Join(wrapped, nested), errors.Join(nested, wrapped)} {
			cases = append(
				cases,
				completionFailureCase{
					fmt.Errorf("private-outer-secret: %w", joined),
					"provider_rate_limited",
					"error",
					429,
				},
				completionFailureCase{errors.Join(joined, core.ErrDatabase), "database_failure", "error", 429},
				completionFailureCase{
					errors.Join(joined, &core.ProblemError{Status: 403}),
					"domain_denied",
					"error",
					429,
				},
			)
		}
	}
	return cases
}
func completionConnectFailure(t *testing.T, cause error) error {
	t.Helper()
	config, err := pgconn.ParseConfig("host=127.0.0.1 user=private-user dbname=private-db sslmode=disable")
	require.NoError(t, err)
	config.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, cause
	}
	_, err = pgconn.ConnectConfig(t.Context(), config)
	var driver *pgconn.ConnectError
	require.ErrorAs(t, err, &driver)
	require.ErrorIs(t, err, cause)
	return err
}

type completionControlPolicy struct {
	admission    delivery.Admission
	observed     delivery.Outcome
	observations int
	observeError error
}

func (p *completionControlPolicy) Admit(context.Context) (delivery.Admission, error) {
	return p.admission, nil
}

func (p *completionControlPolicy) Observe(
	_ context.Context,
	outcome delivery.Outcome,
) (delivery.Outcome, time.Time, error) {
	p.observed = outcome
	p.observations++
	return outcome, time.Now().Add(time.Hour), p.observeError
}

type completionControlScenario struct {
	body, reason, code string
	status, provider   int
	kind               delivery.Kind
	observeError       error
	coarse             string
}

func controlCompletionFailureCases(t *testing.T) []completionFailureCase {
	t.Helper()
	scenarios := []completionControlScenario{
		{
			body:   `{"ok":false,"error_code":401,"description":"private-token-secret"}`,
			status: http.StatusUnauthorized, provider: 401, kind: delivery.Paused, code: "provider_denied",
		},
		{
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":9}}`,
			status: http.StatusTooManyRequests, provider: 429, kind: delivery.Deferred, code: "provider_rate_limited",
		},
		{
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":-1}}`,
			status: http.StatusTooManyRequests, provider: 429, kind: delivery.Parked, code: "provider_rate_limited",
		},
		{
			body:   `{"ok":false,"error_code":401,"description":"private-token-secret"}`,
			status: http.StatusUnauthorized, provider: 401, kind: delivery.Paused, code: "database_failure",
			observeError: core.ErrDatabase,
		},
		{
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":9}}`,
			status: http.StatusTooManyRequests, provider: 429, kind: delivery.Deferred, code: "database_failure",
			observeError: &pgconn.PgError{Code: "42P01", Message: "private-query-secret"},
		},
		{
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":9}}`,
			status: http.StatusTooManyRequests, provider: 429, kind: delivery.Deferred, code: "domain_denied",
			observeError: &core.ProblemError{Status: http.StatusForbidden, Code: "private-domain-secret"},
		},
		{reason: "telegram_service_rejected", code: "provider_control_blocked"},
		{reason: "delivery_cooldown", code: "provider_control_blocked"},
		{reason: "telegram_invalid_cooldown", code: "provider_control_blocked"},
		{reason: "private-admission-secret", code: "provider_control_blocked"},
		{
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":9}}`,
			status: http.StatusTooManyRequests, provider: 429, kind: delivery.Deferred, code: "provider_rate_limited",
			observeError: context.Canceled, coarse: "canceled",
		},
		{
			body:   `{"ok":false,"error_code":429,"parameters":{"retry_after":9}}`,
			status: http.StatusTooManyRequests, provider: 429, kind: delivery.Deferred, code: "provider_rate_limited",
			observeError: context.DeadlineExceeded, coarse: "timeout",
		},
	}
	cases := make([]completionFailureCase, 0, len(scenarios))
	for _, scenario := range scenarios {
		coarse := scenario.coarse
		if coarse == "" {
			coarse = "error"
		}
		cases = append(cases, completionFailureCase{
			err: controlCompletionError(t, scenario), code: scenario.code, coarse: coarse, provider: scenario.provider,
		})
	}
	return cases
}

func controlCompletionError(t *testing.T, scenario completionControlScenario) error {
	t.Helper()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		if scenario.status != 0 {
			w.WriteHeader(scenario.status)
		}
		_, _ = w.Write([]byte(scenario.body))
	}))
	defer server.Close()
	policy := &completionControlPolicy{admission: delivery.Admission{
		Ready: scenario.body != "", Reason: scenario.reason, NotBefore: time.Now().Add(time.Hour),
	}, observeError: scenario.observeError}
	client := telegram.Client{Base: server.URL, Token: "private-token-secret", Control: policy}
	err := client.Call(t.Context(), "getChat", struct{}{}, nil)
	var control *telegram.ControlError
	if scenario.observeError == nil {
		require.ErrorAs(t, err, &control)
	} else {
		require.ErrorIs(t, err, scenario.observeError)
		require.NotErrorAs(t, err, &control, "failed persistence returns the actual wire/persistence join")
	}
	if scenario.body == "" {
		require.Zero(t, calls.Load(), "blocked admission must not dispatch")
		require.Zero(t, policy.observations)
		require.NoError(t, errors.Unwrap(control), "persisted control state does not prove current API provenance")
		require.Equal(t, scenario.reason, control.Reason)
		return err
	}
	require.Equal(t, int64(1), calls.Load())
	require.Equal(t, 1, policy.observations)
	require.Equal(t, scenario.kind, policy.observed.Kind)
	var api *telegram.APIError
	require.ErrorAs(t, err, &api)
	require.Equal(t, scenario.provider, api.Code)
	return err
}

func TestOperationCompletionFailureMetricsAndSpans(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			t.Parallel()
			var sink spanSink
			collectorServer := httptest.NewServer(http.HandlerFunc(sink.serve))
			defer collectorServer.Close()
			runtime, err := observability.New(t.Context(), observability.Config{
				Enabled: enabled, Endpoint: collectorServer.URL, SampleRatio: 1,
			})
			require.NoError(t, err)
			cases := completionFailureCases(t)
			counts := map[string]float64{}
			for _, item := range cases {
				_, finish := runtime.Start(t.Context(), "model.plan")
				finish(fmt.Errorf("private-context-secret: %w", item.err))
				counts[item.code+":"+strconv.Itoa(item.provider)]++
				assertCompletionFailureCounts(t, runtime, counts)
			}
			_, complete := runtime.Start(t.Context(), "model.plan")
			complete(nil)
			response := httptest.NewRecorder()
			runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			require.Equal(t, http.StatusOK, response.Code)
			text := response.Body.String()
			for _, item := range cases {
				assert.Contains(
					t,
					text,
					fmt.Sprintf(
						`code="%s",operation="model.plan",phase="completion",provider_code="%d",retryability="unknown",transport_status="0"`,
						item.code,
						item.provider,
					),
				)
				assert.Contains(t, text, `zns_operations_total{operation="model.plan",result="`+item.coarse+`"}`)
			}
			assert.Contains(t, text, `zns_operations_total{operation="model.plan",result="ok"} 1`)
			assert.NotContains(t, text, "private-")
			assert.NotContains(t, text, "secret")
			shutdown, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			require.NoError(t, runtime.Shutdown(shutdown))
			sink.mu.Lock()
			defer sink.mu.Unlock()
			if !enabled {
				require.Empty(t, sink.spans)
				return
			}
			require.Len(t, sink.spans, len(cases)+1)
			for index, span := range sink.spans[:len(cases)] {
				item := cases[index]
				assert.Equal(t, "model.plan", span.GetName())
				assert.Equal(t, tracepb.Status_STATUS_CODE_ERROR, span.GetStatus().GetCode())
				assert.Equal(t, item.coarse, span.GetStatus().GetMessage())
				assert.Equal(t, map[string]string{"failure.phase": "completion", "failure.code": item.code,
					"failure.retryability": "unknown", "failure.transport_status": "0",
					"failure.provider_code": strconv.Itoa(item.provider)}, completionSpanAttributes(span))
				assert.NotContains(t, span.String(), "private-")
				assert.NotContains(t, span.String(), "secret")
			}
			assert.Empty(t, sink.spans[len(cases)].GetAttributes())
			assert.Equal(t, tracepb.Status_STATUS_CODE_UNSET, sink.spans[len(cases)].GetStatus().GetCode())
		})
	}
}

func TestOperationCompletionNilUnwrapJoined(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			t.Parallel()
			var sink spanSink
			collectorServer := httptest.NewServer(http.HandlerFunc(sink.serve))
			defer collectorServer.Close()
			runtime, err := observability.New(t.Context(), observability.Config{
				Enabled: enabled, Endpoint: collectorServer.URL, SampleRatio: 1,
			})
			require.NoError(t, err)
			defer func() { require.NoError(t, runtime.Shutdown(t.Context())) }()
			for _, invalid := range []error{(*telegram.ControlError)(nil), (*pgconn.ConnectError)(nil)} {
				_, finish := runtime.Start(t.Context(), "model.plan")
				assert.NotPanics(t, func() {
					finish(errors.Join(invalid, &telegram.APIError{Code: http.StatusUnauthorized}))
				})
			}
		})
	}
}

func assertCompletionFailureCounts(t *testing.T, runtime *observability.Runtime, expected map[string]float64) {
	t.Helper()
	families, err := runtime.Registry.Gather()
	require.NoError(t, err)
	actual := map[string]float64{}
	for _, family := range families {
		if family.GetName() != "zns_operation_failures_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := map[string]string{}
			for _, label := range metric.GetLabel() {
				labels[label.GetName()] = label.GetValue()
			}
			actual[labels["code"]+":"+labels["provider_code"]] = metric.GetCounter().GetValue()
		}
	}
	assert.Equal(t, expected, actual, "each completion must increment its own exact provenance series")
}

func completionSpanAttributes(span *tracepb.Span) map[string]string {
	attributes := map[string]string{}
	for _, attr := range span.GetAttributes() {
		value := attr.GetValue()
		if value.GetStringValue() != "" {
			attributes[attr.GetKey()] = value.GetStringValue()
		} else {
			attributes[attr.GetKey()] = strconv.FormatInt(value.GetIntValue(), 10)
		}
	}
	return attributes
}

func TestOperationCompletionFailureCardinality(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			t.Parallel()
			checkCompletionFailureCardinality(t, enabled)
		})
	}
}

func checkCompletionFailureCardinality(t *testing.T, enabled bool) {
	t.Helper()
	var sink spanSink
	collectorServer := httptest.NewServer(http.HandlerFunc(sink.serve))
	defer collectorServer.Close()
	runtime, err := observability.New(t.Context(), observability.Config{
		Enabled: enabled, Endpoint: collectorServer.URL, SampleRatio: 1,
	})
	require.NoError(t, err)
	for index := range 100 {
		for _, failure := range []error{&core.ProblemError{Status: http.StatusForbidden, Code: fmt.Sprintf("private-user-%d", index)},
			&telegram.APIError{Code: 1000 + index, Description: "private-token-secret"}} {
			_, finish := runtime.Start(t.Context(), fmt.Sprintf("private-operation-%d", index))
			finish(failure)
		}
	}
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	text := response.Body.String()
	assert.NotContains(t, text, "private-")
	assert.NotContains(t, text, "secret")
	families, err := runtime.Registry.Gather()
	require.NoError(t, err)
	found := false
	for _, family := range families {
		if family.GetName() == "zns_operation_failures_total" {
			found = true
			require.Len(t, family.GetMetric(), 2, "random codes and operations must not grow the finite dimensions")
			for _, metric := range family.GetMetric() {
				assert.InDelta(t, float64(100), metric.GetCounter().GetValue(), 0)
			}
		}
	}
	require.True(t, found)
	shutdown, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.NoError(t, runtime.Shutdown(shutdown))
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if !enabled {
		require.Empty(t, sink.spans)
		return
	}
	require.Len(t, sink.spans, 200)
	for _, span := range sink.spans {
		assert.Equal(t, "unknown", span.GetName())
		assert.NotContains(t, span.String(), "private-")
		assert.NotContains(t, span.String(), "secret")
	}
}

func TestInvalidTelemetryConfigDoesNotExposeEndpoint(t *testing.T) {
	t.Parallel()
	for _, endpoint := range []string{"http://user:secret@localhost", "http://localhost?token=secret", "invalid-secret"} {
		_, err := observability.New(
			t.Context(),
			observability.Config{Enabled: true, Endpoint: endpoint, SampleRatio: 1},
		)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "secret")
	}
}
