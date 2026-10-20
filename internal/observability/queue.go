package observability

import (
	"context"
	"time"
)

// QueueMetric reports due River jobs, without arguments or business identifiers.
// ScheduledAgeSeconds measures lateness against the current scheduling deadline.
type QueueMetric struct {
	Kind, State         string
	Count               int64
	ScheduledAgeSeconds float64
}

// ObserveQueue registers the task service's due-job reader once at startup.
func (t *Telemetry) ObserveQueue(read func(context.Context) ([]QueueMetric, error)) error {
	return t.observeQueue(read, time.Now)
}

func (t *Telemetry) observeQueue(read func(context.Context) ([]QueueMetric, error), now func() time.Time) error {
	spec := stateMetricSpec{
		prefix:           "zinusya.queue",
		countName:        "due",
		ageName:          "oldest_scheduled_age",
		countDescription: "Due default-queue jobs; scheduled and retryable still require promotion",
		ageDescription:   "Lateness against scheduled_at, including overdue polls and queue pauses",
		states:           []string{"available", "scheduled", "retryable"},
		kinds: []string{
			"shuttle_task",
			"zinusya_payment_notification",
			"zinusya_pair_notification",
			"zinusya_order_notification", "zinusya_knowledge_notification",
			"zinusya_avatar_delivery",
			"zinusya_massage_reminder",
			"zinusya_massage_specialist_notice",
		},
	}
	return t.observeStates(spec, func(ctx context.Context) ([]stateMetric, error) {
		rows, err := read(ctx)
		if err != nil {
			return nil, err
		}
		result := make([]stateMetric, 0, len(rows))
		for _, row := range rows {
			result = append(result, stateMetric{
				State: row.State, Kind: row.Kind, Count: row.Count, AgeSeconds: row.ScheduledAgeSeconds,
			})
		}
		return result, nil
	}, now)
}
