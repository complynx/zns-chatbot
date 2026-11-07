package db

import "context"

// TaskMetric contains only aggregate operational data. Age is signed so future
// timestamps remain distinguishable until the observation is made.
type TaskMetric struct {
	State              string
	Count              int64
	AcceptedAgeSeconds float64
}

// TaskMetrics groups unresolved tasks, including authorization and delivery
// uncertainty. It does not count runnable River jobs or unfinished outbox sends.
func (s *Store) TaskMetrics(ctx context.Context) ([]TaskMetric, error) {
	rows, err := s.q.TaskMetrics(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]TaskMetric, 0, len(rows))
	for _, row := range rows {
		result = append(result, TaskMetric{row.MetricState, row.TaskCount, row.AcceptedAgeSeconds})
	}
	return result, nil
}
