package observability

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"
)

// ObserveDatabasePool registers the application pool once during startup.
// Collection reads one local statistics snapshot; it never acquires a connection.
func (t *Telemetry) ObserveDatabasePool(pool *pgxpool.Pool) error {
	meter := t.meters.Meter("zinusya")
	acquired, err := meter.Int64ObservableGauge(
		"zinusya.db.pool.acquired",
		metric.WithDescription("Connections acquired by application and queue work, including the instance lock"),
	)
	if err != nil {
		return err
	}
	idle, err := meter.Int64ObservableGauge(
		"zinusya.db.pool.idle",
		metric.WithDescription("Idle connections in the application pool"),
	)
	if err != nil {
		return err
	}
	total, err := meter.Int64ObservableGauge(
		"zinusya.db.pool.total",
		metric.WithDescription("Pool connections, including connections under construction"),
	)
	if err != nil {
		return err
	}
	maximum, err := meter.Int64ObservableGauge(
		"zinusya.db.pool.max",
		metric.WithDescription("Configured maximum connections in the application pool"),
	)
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		stat := pool.Stat()
		observer.ObserveInt64(acquired, int64(stat.AcquiredConns()))
		observer.ObserveInt64(idle, int64(stat.IdleConns()))
		observer.ObserveInt64(total, int64(stat.TotalConns()))
		observer.ObserveInt64(maximum, int64(stat.MaxConns()))
		return nil
	}, acquired, idle, total, maximum)
	return err
}
