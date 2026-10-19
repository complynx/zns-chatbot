package db

import "context"

// OutboxMetric contains only aggregate operational data. Age is signed so future
// timestamps remain distinguishable until the observation is made.
type OutboxMetric struct {
	State             string
	Count             int64
	CreatedAgeSeconds float64
}

// OutboxMetrics counts unfinished chunks independently of task state. External
// uncertainty can remain after local task closure; permanent failures are excluded.
func (s *Store) OutboxMetrics(ctx context.Context) ([]OutboxMetric, error) {
	rows, err := s.q.OutboxMetrics(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]OutboxMetric, 0, len(rows))
	for _, row := range rows {
		result = append(result, OutboxMetric{row.MetricState, row.ChunkCount, row.CreatedAgeSeconds})
	}
	return result, nil
}
