package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func newWorkerDuration(meter metric.Meter) (metric.Float64Histogram, error) {
	return meter.Float64Histogram(
		"zinusya.worker.attempt.duration",
		metric.WithUnit("s"),
		metric.WithDescription(
			"Normally returned Work callbacks, including callback-owned cleanup; not business completion",
		),
		//nolint:mnd // Explicit latency boundaries in seconds, including the default 20s attempt and 22s River timeout.
		metric.WithExplicitBucketBoundaries(.005, .01, .025, .05, .1, .25, .5, 1, 2, 5, 10, 15, 20, 22, 30, 60),
	)
}

// RecordWorkerDuration records one normally returned callback, not its later River disposition.
// The task package classifies the return; unknown labels cannot expose caller data.
func (t *Telemetry) RecordWorkerDuration(ctx context.Context, kind, outcome string, duration time.Duration) {
	t.workerDuration.Record(ctx, duration.Seconds(), metric.WithAttributes(
		attribute.String("kind", boundedWorkerKind(kind)),
		attribute.String("outcome", boundedWorkerOutcome(outcome)),
	))
}

func boundedWorkerKind(kind string) string {
	switch kind {
	case "shuttle_task",
		"zinusya_payment_notification",
		"zinusya_pair_notification",
		"zinusya_order_notification", "zinusya_knowledge_notification", "zinusya_avatar_delivery",
		"zinusya_massage_reminder", "zinusya_massage_specialist_notice":
		return kind
	default:
		return otherLabel
	}
}

func boundedWorkerOutcome(outcome string) string {
	switch outcome {
	case "returned", "snoozed", "cancelled", errorLabel:
		return outcome
	default:
		return otherLabel
	}
}
