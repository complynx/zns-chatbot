package observability

import (
	"context"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

const (
	stateMetricsInterval = 30 * time.Second
	stateMetricsTimeout  = 250 * time.Millisecond
)

type stateMetric struct {
	State      string
	Kind       string
	Count      int64
	AgeSeconds float64
}

type stateMetricSpec struct {
	prefix           string
	countName        string
	ageName          string
	countDescription string
	ageDescription   string
	states           []string
	kinds            []string
}

type stateMetricSnapshot struct {
	rows          []stateMetric
	completedAt   time.Time
	lastSuccessAt time.Time
	valid         bool
}

type stateMetricCache struct {
	states  []string
	kinds   []string
	read    func(context.Context) ([]stateMetric, error)
	now     func() time.Time
	refresh sync.Mutex
	latest  atomic.Pointer[stateMetricSnapshot]
}

// sample publishes complete immutable generations. Failed attempts are throttled
// too, and replace successful samples rather than hiding failure behind old rows.
func (c *stateMetricCache) sample(ctx context.Context) *stateMetricSnapshot {
	previous := c.latest.Load()
	if !stateRefreshDue(previous, c.now()) || !c.refresh.TryLock() {
		return previous
	}
	defer c.refresh.Unlock()
	previous = c.latest.Load()
	if !stateRefreshDue(previous, c.now()) {
		return previous
	}
	query, cancel := context.WithTimeout(ctx, stateMetricsTimeout)
	rows, err := c.read(query)
	withinBudget := query.Err() == nil
	cancel()
	next := &stateMetricSnapshot{completedAt: c.now()}
	if previous != nil {
		next.lastSuccessAt = previous.lastSuccessAt
	}
	if err == nil && withinBudget {
		next.rows, next.valid = completeStateMetrics(rows, c.states, c.kinds)
		if next.valid {
			next.lastSuccessAt = next.completedAt
		}
	}
	c.latest.Store(next)
	return next
}

func stateRefreshDue(snapshot *stateMetricSnapshot, now time.Time) bool {
	return snapshot == nil || now.Sub(snapshot.completedAt) >= stateMetricsInterval
}

func completeStateMetrics(rows []stateMetric, states, kinds []string) ([]stateMetric, bool) {
	if len(kinds) == 0 {
		kinds = []string{""}
	}
	if len(rows) > len(states)*len(kinds) {
		return nil, false
	}
	result := make([]stateMetric, 0, len(states)*len(kinds))
	for _, state := range states {
		for _, kind := range kinds {
			result = append(result, stateMetric{State: state, Kind: kind})
		}
	}
	seen := make([]bool, len(result))
	for _, row := range rows {
		index := slices.IndexFunc(result, func(candidate stateMetric) bool {
			return candidate.State == row.State && candidate.Kind == row.Kind
		})
		if index < 0 || seen[index] || row.Count <= 0 || math.IsNaN(row.AgeSeconds) ||
			math.IsInf(row.AgeSeconds, 0) {
			return nil, false
		}
		result[index] = row
		seen[index] = true
	}
	return result, true
}
