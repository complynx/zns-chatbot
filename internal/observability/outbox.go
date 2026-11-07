package observability

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
)

// ObserveOutbox registers one deployment-wide unfinished-delivery snapshot at startup.
// A refresh has a separate acquisition/query deadline; OTel can serialize scrapes.
func (t *Telemetry) ObserveOutbox(store *db.Store) error {
	return t.observeOutbox(store.OutboxMetrics, time.Now)
}

func (t *Telemetry) observeOutbox(read func(context.Context) ([]db.OutboxMetric, error), now func() time.Time) error {
	spec := stateMetricSpec{prefix: "zinusya.outbox", ageName: "oldest_created_age",
		countDescription: "Unfinished outbox chunks by state, including external uncertainty",
		ageDescription:   "Oldest age since outbox row creation, not time in state or runnable latency",
		states:           []string{"pending", "retryable", "send_inflight", "delivery_unknown", "other"},
	}
	return t.observeStates(spec, func(ctx context.Context) ([]stateMetric, error) {
		rows, err := read(ctx)
		if err != nil {
			return nil, err
		}
		result := make([]stateMetric, 0, len(rows))
		for _, row := range rows {
			result = append(result, stateMetric{State: row.State, Count: row.Count, AgeSeconds: row.CreatedAgeSeconds})
		}
		return result, nil
	}, now)
}
