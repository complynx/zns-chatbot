package observability

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
)

// ObserveTasks registers one deployment-wide unresolved-task snapshot at startup.
// A refresh has a separate acquisition/query deadline; OTel can serialize scrapes.
func (t *Telemetry) ObserveTasks(store *db.Store) error {
	return t.observeTasks(store.TaskMetrics, time.Now)
}

func (t *Telemetry) observeTasks(read func(context.Context) ([]db.TaskMetric, error), now func() time.Time) error {
	spec := stateMetricSpec{
		prefix:           "zinusya.tasks",
		ageName:          "oldest_accepted_age",
		countDescription: "Unresolved tasks by operational state, not runnable jobs",
		ageDescription:   "Oldest age since acceptance per unresolved state, including intentional waits",
		states: []string{
			"accepted",
			"creating_run",
			"running",
			"awaiting_service",
			"awaiting_authorization",
			"auth_reauthorizing",
			"awaiting_user",
			"auth_unknown",
			"delivery_unknown",
			"other",
		},
	}
	return t.observeStates(spec, func(ctx context.Context) ([]stateMetric, error) {
		rows, err := read(ctx)
		if err != nil {
			return nil, err
		}
		result := make([]stateMetric, 0, len(rows))
		for _, row := range rows {
			result = append(result, stateMetric{State: row.State, Count: row.Count, AgeSeconds: row.AcceptedAgeSeconds})
		}
		return result, nil
	}, now)
}
