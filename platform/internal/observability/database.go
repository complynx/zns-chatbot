package observability

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

type queryKey struct{}

// PGXTracer records query duration/result without SQL, arguments, or connection details.
func (r *Runtime) PGXTracer() pgx.QueryTracer { return r }

func (r *Runtime) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	ctx, finish := r.Start(ctx, "db")
	return context.WithValue(ctx, queryKey{}, finish)
}

func (r *Runtime) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if finish, ok := ctx.Value(queryKey{}).(func(error)); ok {
		finish(data.Err)
	}
}

// RegisterPool registers one pool's aggregate gauges. Call before serving metrics.
func (r *Runtime) RegisterPool(pool *pgxpool.Pool) error {
	return r.Registry.Register(&poolCollector{
		pool:  pool,
		total: prometheus.NewDesc("zns_db_pool_connections", "Current pool connections.", []string{"state"}, nil),
		waits: prometheus.NewDesc(
			"zns_db_pool_acquire_wait_seconds_total",
			"Time spent acquiring pool connections.",
			nil,
			nil,
		),
	})
}

type poolCollector struct {
	pool  *pgxpool.Pool
	total *prometheus.Desc
	waits *prometheus.Desc
}

func (c *poolCollector) Describe(out chan<- *prometheus.Desc) {
	out <- c.total
	out <- c.waits
}

func (c *poolCollector) Collect(out chan<- prometheus.Metric) {
	stats := c.pool.Stat()
	out <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(stats.AcquiredConns()), "acquired")
	out <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(stats.IdleConns()), "idle")
	out <- prometheus.MustNewConstMetric(c.total, prometheus.GaugeValue, float64(stats.ConstructingConns()), "constructing")
	out <- prometheus.MustNewConstMetric(c.waits, prometheus.CounterValue, stats.AcquireDuration().Seconds())
}
