package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/complynx/zns-chatbot/internal/identity"
)

// ObserveIdentity registers local cumulative cache decisions once during startup.
func (t *Telemetry) ObserveIdentity(client *identity.Client) error {
	meter := t.meters.Meter("zinusya")
	lookups, err := meter.Int64ObservableCounter(
		"zinusya.identity.positive_cache.lookups",
		metric.WithDescription("Positive introspection cache lookups after JWT trust validation"),
	)
	if err != nil {
		return err
	}
	fallbacks, err := meter.Int64ObservableCounter(
		"zinusya.identity.token.fallbacks",
		metric.WithDescription(
			"Successful singleflight decisions using an unexpired credential after transient renewal failure",
		),
	)
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		stats := client.CacheStats()
		observer.ObserveInt64(lookups, stats.PositiveHits, metric.WithAttributes(attribute.String("outcome", "hit")))
		observer.ObserveInt64(lookups, stats.PositiveMisses, metric.WithAttributes(attribute.String("outcome", "miss")))
		observer.ObserveInt64(
			fallbacks,
			stats.ServiceFallbacks,
			metric.WithAttributes(attribute.String("kind", "service")),
		)
		observer.ObserveInt64(fallbacks, stats.UserFallbacks, metric.WithAttributes(attribute.String("kind", "user")))
		return nil
	}, lookups, fallbacks)
	return err
}
