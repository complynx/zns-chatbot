package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type stateGauges struct {
	count       metric.Int64ObservableGauge
	age         metric.Float64ObservableGauge
	success     metric.Int64ObservableGauge
	lastSuccess metric.Float64ObservableGauge
	freshness   metric.Float64ObservableGauge
}

// observeStates creates an independent cache and callback for each fixed family.
func (t *Telemetry) observeStates(
	spec stateMetricSpec,
	read func(context.Context) ([]stateMetric, error),
	now func() time.Time,
) error {
	gauges, err := newStateGauges(t.meters.Meter("zinusya"), spec)
	if err != nil {
		return err
	}
	cache := &stateMetricCache{read: read, now: now, states: spec.states, kinds: spec.kinds}
	_, err = t.meters.Meter("zinusya").RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		gauges.observe(observer, cache.sample(ctx), now())
		return nil
	}, gauges.count, gauges.age, gauges.success, gauges.lastSuccess, gauges.freshness)
	return err
}

func newStateGauges(meter metric.Meter, spec stateMetricSpec) (stateGauges, error) {
	var gauges stateGauges
	var err error
	countName := spec.countName
	if countName == "" {
		countName = "unresolved"
	}
	gauges.count, err = meter.Int64ObservableGauge(
		spec.prefix+"."+countName, metric.WithDescription(spec.countDescription),
	)
	if err != nil {
		return gauges, err
	}
	gauges.age, err = meter.Float64ObservableGauge(
		spec.prefix+"."+spec.ageName, metric.WithUnit("s"),
		metric.WithDescription(spec.ageDescription),
	)
	if err != nil {
		return gauges, err
	}
	gauges.success, err = meter.Int64ObservableGauge(
		spec.prefix+".collection_success",
		metric.WithDescription("Whether the latest completed snapshot attempt succeeded"),
	)
	if err != nil {
		return gauges, err
	}
	gauges.lastSuccess, err = meter.Float64ObservableGauge(
		spec.prefix+".last_success_timestamp", metric.WithUnit("s"),
		metric.WithDescription("Unix timestamp of the last successful collection, or zero before success"),
	)
	if err != nil {
		return gauges, err
	}
	gauges.freshness, err = meter.Float64ObservableGauge(
		spec.prefix+".snapshot_age", metric.WithUnit("s"),
		metric.WithDescription("Elapsed time since the last successful collection; absent before success"),
	)
	return gauges, err
}

func (g stateGauges) observe(observer metric.Observer, snapshot *stateMetricSnapshot, now time.Time) {
	var success int64
	var lastSuccess float64
	if snapshot != nil {
		if snapshot.valid {
			success = 1
		}
		if !snapshot.lastSuccessAt.IsZero() {
			lastSuccess = float64(snapshot.lastSuccessAt.Unix()) +
				float64(snapshot.lastSuccessAt.Nanosecond())/float64(time.Second)
			observer.ObserveFloat64(g.freshness, max(0, now.Sub(snapshot.lastSuccessAt).Seconds()))
		}
	}
	observer.ObserveInt64(g.success, success)
	observer.ObserveFloat64(g.lastSuccess, lastSuccess)
	if success == 0 {
		return
	}
	elapsed := max(0, now.Sub(snapshot.completedAt).Seconds())
	for _, row := range snapshot.rows {
		var age float64
		if row.Count > 0 {
			age = max(0, row.AgeSeconds+elapsed)
		}
		attributes := []attribute.KeyValue{attribute.String("state", row.State)}
		if row.Kind != "" {
			attributes = append(attributes, attribute.String("kind", row.Kind))
		}
		labels := metric.WithAttributes(attributes...)
		observer.ObserveInt64(g.count, row.Count, labels)
		observer.ObserveFloat64(g.age, age, labels)
	}
}
