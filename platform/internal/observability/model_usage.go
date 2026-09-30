package observability

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

// RegisterModelUsage observes existing durable accounting. It starts no model
// calls, does not charge users, and must be registered once per process.
func (r *Runtime) RegisterModelUsage(service credits.Service) error {
	if service.DB == nil {
		return errors.New("model usage observation configuration missing")
	}
	return r.Registry.Register(newModelUsageCollector(service.ObserveUsage))
}

type modelUsageCollector struct {
	read                                          func(context.Context) (credits.UsageObservation, error)
	available, truncated, limit, lookback         *prometheus.Desc
	states, bases, tokens, known, unknown, capped *prometheus.Desc
}

func newModelUsageCollector(read func(context.Context) (credits.UsageObservation, error)) *modelUsageCollector {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc("zns_model_usage_sample_"+name, help, labels, nil)
	}
	return &modelUsageCollector{
		read:      read,
		available: desc("available", "Whether the current bounded durable usage sample was read successfully."),
		truncated: desc(
			"truncated",
			"Whether more recent-window attempts exist than the sample includes; totals are sample-only.",
		),
		limit: desc("limit", "Maximum newest attempts included in this sample."),
		lookback: desc(
			"lookback_seconds",
			"Creation-time lookback for newest sampled attempts; not settlement time or lifetime usage.",
		),
		states: desc(
			"attempts",
			"Sampled durable attempts by accounting state; settled is not provider success.",
			"state",
		),
		bases: desc(
			"settled_receipts",
			"Sampled settled receipts by persisted usage basis, including unknown.",
			"basis",
		),
		tokens: desc(
			"tokens",
			"Known token sum in sampled settled receipts, capped at exact float integer precision; subsets overlap totals.",
			"kind",
		),
		known: desc(
			"known_receipts",
			"Sampled settled receipts with a known token counter, including known zero.",
			"kind",
		),
		unknown: desc(
			"unknown_receipts",
			"Sampled settled receipts missing this token counter; missing is not zero.",
			"kind",
		),
		capped: desc(
			"tokens_capped",
			"Whether the sampled known token sum exceeds exact float integer precision.",
			"kind",
		),
	}
}

func (c *modelUsageCollector) Describe(out chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.available, c.truncated, c.limit, c.lookback, c.states, c.bases, c.tokens, c.known, c.unknown, c.capped} {
		out <- desc
	}
}

func (c *modelUsageCollector) Collect(out chan<- prometheus.Metric) {
	const timeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	item, err := c.read(ctx)
	if err != nil || !validModelUsage(item) {
		out <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 0)
		return
	}
	emit := func(desc *prometheus.Desc, value float64, labels ...string) {
		out <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
	}
	emit(c.available, 1)
	emit(c.truncated, usageBool(item.Truncated))
	emit(c.limit, credits.UsageObservationLimit)
	emit(c.lookback, credits.UsageObservationLookback.Seconds())
	for i, state := range []string{"reserved", "dispatched", "settled", "not_sent"} {
		emit(c.states, float64(item.States[i]), state)
	}
	for i, basis := range []string{"reported", "estimated", "unknown", "known_free"} {
		emit(c.bases, float64(item.Bases[i]), basis)
	}
	for i, kind := range []string{"input", "cached_input", "cache_write", "output", "reasoning", "audio_input", "text_input"} {
		value := item.Tokens[i]
		emit(c.tokens, float64(value.Sum), kind)
		emit(c.known, float64(value.KnownReceipts), kind)
		emit(c.unknown, float64(value.UnknownReceipts), kind)
		emit(c.capped, usageBool(value.Capped), kind)
	}
}

func usageBool(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func validModelUsage(item credits.UsageObservation) bool {
	var total, bases int64
	for _, count := range item.States {
		if count < 0 || count > credits.UsageObservationLimit {
			return false
		}
		total += count
	}
	if total > credits.UsageObservationLimit || (item.Truncated && total != credits.UsageObservationLimit) {
		return false
	}
	for _, count := range item.Bases {
		if count < 0 || count > credits.UsageObservationLimit {
			return false
		}
		bases += count
	}
	if bases != item.States[2] {
		return false
	}
	for _, value := range item.Tokens {
		if value.Sum < 0 || value.Sum > credits.UsageObservationExactMaximum || value.KnownReceipts < 0 ||
			value.UnknownReceipts < 0 ||
			value.KnownReceipts > credits.UsageObservationLimit ||
			value.UnknownReceipts > credits.UsageObservationLimit ||
			value.KnownReceipts+value.UnknownReceipts != item.States[2] ||
			(value.Capped && value.Sum != credits.UsageObservationExactMaximum) ||
			(value.KnownReceipts == 0 && (value.Sum != 0 || value.Capped)) {
			return false
		}
	}
	return true
}
