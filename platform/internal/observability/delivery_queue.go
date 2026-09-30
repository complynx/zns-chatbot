package observability

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// RegisterDeliveryQueue exposes one bot's bounded aggregate queue observations.
// Register before serving metrics. Bot identity is a query parameter, never a label.
func (r *Runtime) RegisterDeliveryQueue(pool *pgxpool.Pool, botID int64) error {
	if pool == nil || botID <= 0 {
		return errors.New("delivery observation configuration missing")
	}
	return r.Registry.Register(
		newDeliveryQueueCollector(func(ctx context.Context) ([]delivery.QueueObservation, error) {
			return delivery.QueueObservations(ctx, pool, botID)
		}),
	)
}

type deliveryQueueCollector struct {
	read       func(context.Context) ([]delivery.QueueObservation, error)
	available  *prometheus.Desc
	dimensions []*prometheus.Desc
}

func newDeliveryQueueCollector(
	read func(context.Context) ([]delivery.QueueObservation, error),
) *deliveryQueueCollector {
	collector := &deliveryQueueCollector{
		read: read,
		available: prometheus.NewDesc(
			"zns_delivery_snapshot_available",
			"Whether the current queue snapshot succeeded.",
			nil,
			nil,
		),
	}
	for _, metric := range []struct{ name, help string }{
		{"backlog", "Active durable deliveries, including blocked lane followers."},
		{"age_unknown", "Active deliveries without a proven enqueue time."},
		{"oldest_age_seconds", "Oldest known enqueue age, capped at one year; zero if none known."},
		{"delayed", "Pending deliveries with a future pacing or queue deadline."},
		{"paused", "Active deliveries paused or parked by queue or shared pacing policy."},
		{"unbounded_deadline", "Active deliveries with an infinite not-before deadline."},
		{"next_attempt_seconds", "Earliest future pending deadline without a pause, capped at one year; zero if absent. Advisory, not lane eligibility."},
		{"maximum_wait_seconds", "Longest finite pending pacing or queue wait, capped at one year."},
	} {
		collector.dimensions = append(
			collector.dimensions,
			prometheus.NewDesc(
				"zns_delivery_queue_"+metric.name,
				metric.help,
				[]string{"owner", "class", "state"},
				nil,
			),
		)
	}
	return collector
}

func (c *deliveryQueueCollector) Describe(out chan<- *prometheus.Desc) {
	out <- c.available
	for _, dimension := range c.dimensions {
		out <- dimension
	}
}

func (c *deliveryQueueCollector) Collect(out chan<- prometheus.Metric) {
	items, err := c.read(context.Background())
	if err != nil || !validQueueObservations(items) {
		out <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 0)
		return
	}
	out <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 1)
	for _, item := range items {
		values := []float64{float64(item.Count), float64(item.UnknownAge), item.OldestAgeSeconds,
			float64(item.Delayed), float64(item.Paused), float64(item.UnboundedDeadline),
			item.NextAttemptSeconds, item.MaximumWaitSeconds}
		for index, value := range values {
			out <- prometheus.MustNewConstMetric(c.dimensions[index], prometheus.GaugeValue, value,
				string(item.Owner), string(item.Class), string(item.State))
		}
	}
}

func validQueueObservations(items []delivery.QueueObservation) bool {
	const maximum = 7 * 2 * 5
	if len(items) > maximum {
		return false
	}
	seen := make(map[[3]string]bool, len(items))
	for _, item := range items {
		key := [3]string{string(item.Owner), string(item.Class), string(item.State)}
		if !item.Valid() || seen[key] {
			return false
		}
		seen[key] = true
	}
	return true
}
