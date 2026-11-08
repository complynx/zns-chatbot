package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ObserveDescriptor reports an optional component independently of core intake readiness.
// Running means the loop is alive; it does not prove provider credentials or funding.
func (t *Telemetry) ObserveDescriptor(enabled bool, snapshot func() (string, int64)) error {
	meter := t.meters.Meter("zinusya")
	active, err := meter.Int64ObservableGauge("zinusya.descriptor.enabled")
	if err != nil {
		return err
	}
	state, err := meter.Int64ObservableGauge("zinusya.descriptor.state")
	if err != nil {
		return err
	}
	blocked, err := meter.Int64ObservableGauge("zinusya.descriptor.blocked")
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		current, count, on := "stopped", int64(0), int64(0)
		if enabled {
			on = 1
			current, count = snapshot()
		}
		observer.ObserveInt64(active, on)
		observer.ObserveInt64(blocked, count)
		for _, value := range []string{"running", "retrying", "blocked", "stopped"} {
			var selected int64
			if current == value {
				selected = 1
			}
			observer.ObserveInt64(state, selected, metric.WithAttributes(attribute.String("state", value)))
		}
		return nil
	}, active, state, blocked)
	return err
}
