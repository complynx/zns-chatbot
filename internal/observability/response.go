package observability

import (
	"context"
	"math"

	"go.opentelemetry.io/otel/metric"
)

func newResponseLatency(meter metric.Meter) (metric.Float64Histogram, error) {
	return meter.Float64Histogram(
		"zinusya.response.confirmed.duration",
		metric.WithUnit("s"),
		metric.WithDescription(
			"Root task row creation to persisted Telegram final-response receipt, including user and service waits",
		),
		//nolint:mnd // Seconds span immediate replies through intentional multi-day waits.
		metric.WithExplicitBucketBoundaries(1, 5, 15, 30, 60, 300, 900, 3600, 21600, 86400, 604800),
	)
}

// RecordResponseLatency observes a newly committed final-response receipt once.
// Export is process-local and best effort; timestamps remain durable in the DB.
// Clock anomalies are omitted rather than reported as zero or negative latency.
func (t *Telemetry) RecordResponseLatency(ctx context.Context, seconds float64) {
	if seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return
	}
	t.responseLatency.Record(ctx, seconds)
}
