package observability

import (
	"errors"
	"net/http"

	"github.com/felixge/httpsnoop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HTTPHandler preserves optional ResponseWriter interfaces via httpsnoop.
// The operation is a bounded name, never a request path or user identifier.
func (r *Runtime) HTTPHandler(operation string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		ctx := r.propagation.Extract(request.Context(), propagation.HeaderCarrier(request.Header))
		ctx, finish := r.start(ctx, operation, trace.SpanKindServer)
		var failure error
		defer func() { finish(failure) }()
		panicking := true
		defer func() {
			if panicking {
				failure = errors.New("http handler panic")
			}
		}()
		metrics := httpsnoop.CaptureMetrics(next, w, request.WithContext(ctx))
		panicking = false
		if metrics.Code >= http.StatusInternalServerError {
			failure = errors.New("http server failure")
		}
	})
}

type transport struct {
	runtime   *Runtime
	base      http.RoundTripper
	operation string
}

// HTTPClient wraps a transport without changing the caller's request or response.
// Only W3C trace context is injected; baggage is never propagated.
func (r *Runtime) HTTPClient(operation string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{runtime: r, base: base, operation: operationName(operation)}
}

func (t *transport) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx, finish := t.runtime.start(request.Context(), t.operation, trace.SpanKindClient)
	clone := request.Clone(ctx)
	clone.Header.Del("Baggage")
	t.runtime.propagation.Inject(ctx, propagation.HeaderCarrier(clone.Header))
	response, err := t.base.RoundTrip(clone)
	failure := err
	if err == nil && response != nil && response.StatusCode >= http.StatusInternalServerError {
		failure = errors.New("http client failure")
	}
	finish(failure)
	return response, err
}
