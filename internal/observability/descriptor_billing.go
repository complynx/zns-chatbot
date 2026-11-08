package observability

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/complynx/zns-chatbot/internal/loom"
)

// ObserveDescriptorBilling exports a background probe snapshot, never an HTTP read.
// Nonzero available credit is not proof that the next reservation can be covered.
func (t *Telemetry) ObserveDescriptorBilling(snapshot func() (loom.CreditBalance, bool, time.Time)) error {
	meter := t.meters.Meter("zinusya")
	available, err := meter.Int64ObservableGauge("zinusya.descriptor.billing.available_microcredits")
	if err != nil {
		return err
	}
	reserved, err := meter.Int64ObservableGauge("zinusya.descriptor.billing.reserved_microcredits")
	if err != nil {
		return err
	}
	success, err := meter.Int64ObservableGauge("zinusya.descriptor.billing.collection_success")
	if err != nil {
		return err
	}
	updated, err := meter.Float64ObservableGauge("zinusya.descriptor.billing.last_success_timestamp", metric.WithUnit("s"))
	if err != nil {
		return err
	}
	_, err = meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		balance, ok, at := snapshot()
		var healthy int64
		if ok {
			healthy = 1
			observer.ObserveInt64(available, balance.AvailableMicrocredits)
			observer.ObserveInt64(reserved, balance.ReservedMicrocredits)
		}
		observer.ObserveInt64(success, healthy)
		var timestamp float64
		if !at.IsZero() {
			timestamp = float64(at.Unix())
		}
		observer.ObserveFloat64(updated, timestamp)
		return nil
	}, available, reserved, success, updated)
	return err
}
