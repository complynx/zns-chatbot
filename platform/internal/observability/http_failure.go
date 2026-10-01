package observability

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	httpFailureMetricName     = "zns_http_boundary_failures_total"
	httpFailureMetricHelp     = "HTTP boundary failures by safe classification; retryability is unknown because transport alone cannot authorize replay."
	maxHTTPTransportStatus    = 599
	httpFailureOperationLabel = "operation"
	httpFailureCanceled       = "canceled"
	httpFailureTimeout        = "timeout"
)

// This observation describes the HTTP boundary only. A status cannot prove
// safe replay of a domain command or actual notification delivery.
type httpFailure struct {
	phase  string
	code   string
	status string
}

func registerHTTPFailures(registry *prometheus.Registry) *prometheus.CounterVec {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: httpFailureMetricName,
		Help: httpFailureMetricHelp,
	}, []string{httpFailureOperationLabel, "phase", "code", "retryability", "transport_status"})
	if err := registry.Register(counter); err != nil {
		if duplicate, ok := errors.AsType[prometheus.AlreadyRegisteredError](err); ok {
			if existing, compatible := duplicate.ExistingCollector.(*prometheus.CounterVec); compatible {
				return existing
			}
		}
		// Observation registration must never turn an HTTP call into a failure.
		return nil
	}
	return counter
}

func observeHTTPFailure(counter *prometheus.CounterVec, operation string, response *http.Response, err error) {
	if counter == nil {
		return
	}
	failure, exists := classifyHTTPFailure(response, err)
	if !exists {
		return
	}
	// Registry registration verifies label names; named values also preserve their
	// meaning if a compatible existing CounterVec declares them in another order.
	counter.With(prometheus.Labels{
		httpFailureOperationLabel: operationName(operation), "phase": failure.phase, "code": failure.code,
		"retryability": diagnosticUnknown, "transport_status": failure.status,
	}).Inc()
}

func classifyHTTPFailure(response *http.Response, err error) (httpFailure, bool) {
	status := "0"
	if response != nil && response.StatusCode >= http.StatusContinue && response.StatusCode <= maxHTTPTransportStatus {
		status = strconv.Itoa(response.StatusCode)
	}
	if err != nil {
		code := "transport_unavailable"
		switch {
		case errors.Is(err, context.Canceled):
			code = httpFailureCanceled
		case errors.Is(err, context.DeadlineExceeded):
			code = httpFailureTimeout
		default:
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				code = httpFailureTimeout
			}
		}
		return httpFailure{phase: "roundtrip", code: code, status: status}, true
	}
	if response == nil || status == "0" {
		return httpFailure{phase: "roundtrip", code: "invalid_transport_response", status: "0"}, true
	}
	var code string
	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		code = "authorization_denied"
	case response.StatusCode == http.StatusTooManyRequests:
		code = "rate_limited"
	case response.StatusCode >= http.StatusInternalServerError:
		code = "remote_failure"
	case response.StatusCode >= http.StatusBadRequest:
		code = "request_rejected"
	default:
		return httpFailure{}, false
	}
	return httpFailure{phase: "response", code: code, status: status}, true
}
