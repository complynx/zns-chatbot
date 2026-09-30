package observability

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// RegisterAssistantSources observes the existing typed owner projection. It
// does not start a refresh runner, load a resource or inspect source documents.
func (r *Runtime) RegisterAssistantSources(service knowledge.Service) error {
	if service.DB == nil {
		return errors.New("assistant source observation configuration missing")
	}
	return r.Registry.Register(newAssistantSourceCollector(service.SourceStatuses, time.Now))
}

type assistantSourceCollector struct {
	read          func(context.Context) ([]knowledge.SourceStatus, error)
	now           func() time.Time
	available     *prometheus.Desc
	present       *prometheus.Desc
	state         *prometheus.Desc
	lastError     *prometheus.Desc
	version       *prometheus.Desc
	versionCapped *prometheus.Desc
	attemptKnown  *prometheus.Desc
	refreshKnown  *prometheus.Desc
	attemptAge    *prometheus.Desc
	refreshAge    *prometheus.Desc
}

func newAssistantSourceCollector(
	read func(context.Context) ([]knowledge.SourceStatus, error),
	now func() time.Time,
) *assistantSourceCollector {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc("zns_assistant_source_"+name, help, labels, nil)
	}
	return &assistantSourceCollector{
		read:      read,
		now:       now,
		available: desc("snapshot_available", "Whether the current source status read succeeded."),
		present: desc(
			"present",
			"Whether the source registry has a row; absence is not a refresh failure.",
			"source",
		),
		state: desc(
			"state",
			"Current persisted source state; unavailable does not by itself mean a provider failure.",
			"source",
			"state",
		),
		lastError: desc("last_error", "Last persisted fixed refresh error code, or none.", "source", "code"),
		version: desc(
			"version",
			"Current source version, capped at the largest exact Prometheus integer.",
			"source",
		),
		versionCapped: desc(
			"version_capped",
			"Whether the exact durable version exceeds the reported float integer bound.",
			"source",
		),
		attemptKnown: desc(
			"attempt_age_known",
			"Whether the source has a persisted last attempt timestamp.",
			"source",
		),
		refreshKnown: desc(
			"refresh_age_known",
			"Whether the source has a persisted last successful refresh timestamp.",
			"source",
		),
		attemptAge: desc(
			"attempt_age_seconds",
			"Age of the last refresh attempt, capped at one year; zero when unknown or ahead of the observer clock.",
			"source",
		),
		refreshAge: desc(
			"refresh_age_seconds",
			"Age of the last successful refresh, capped at one year; zero when unknown or ahead of the observer clock.",
			"source",
		),
	}
}

func (c *assistantSourceCollector) Describe(out chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.available, c.present, c.state, c.lastError, c.version, c.versionCapped,
		c.attemptKnown, c.refreshKnown, c.attemptAge, c.refreshAge} {
		out <- desc
	}
}

func (c *assistantSourceCollector) Collect(out chan<- prometheus.Metric) {
	const timeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	items, err := c.read(ctx)
	if err != nil || !validSourceStatuses(items) {
		out <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 0)
		return
	}
	out <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 1)
	bySlot := make(map[string]knowledge.SourceStatus, len(items))
	for _, item := range items {
		bySlot[item.Slot] = item
	}
	now := c.now()
	const maximumExactInteger int64 = 1<<53 - 1
	for _, slot := range []string{knowledge.AssistantQA, knowledge.AssistantAbout} {
		item, exists := bySlot[slot]
		present := float64(0)
		if exists {
			present = 1
		}
		out <- prometheus.MustNewConstMetric(c.present, prometheus.GaugeValue, present, slot)
		if !exists {
			continue
		}
		code := item.ErrorCode
		if code == "" {
			code = "none"
		}
		out <- prometheus.MustNewConstMetric(c.state, prometheus.GaugeValue, 1, slot, item.Status)
		out <- prometheus.MustNewConstMetric(c.lastError, prometheus.GaugeValue, 1, slot, code)
		out <- prometheus.MustNewConstMetric(c.version, prometheus.GaugeValue, float64(min(item.Version, maximumExactInteger)), slot)
		capped := float64(0)
		if item.Version > maximumExactInteger {
			capped = 1
		}
		out <- prometheus.MustNewConstMetric(c.versionCapped, prometheus.GaugeValue, capped, slot)
		attemptKnown, attemptAge := sourceTimestampAge(now, item.AttemptedAt)
		refreshKnown, refreshAge := sourceTimestampAge(now, item.RefreshedAt)
		out <- prometheus.MustNewConstMetric(c.attemptKnown, prometheus.GaugeValue, attemptKnown, slot)
		out <- prometheus.MustNewConstMetric(c.refreshKnown, prometheus.GaugeValue, refreshKnown, slot)
		out <- prometheus.MustNewConstMetric(c.attemptAge, prometheus.GaugeValue, attemptAge, slot)
		out <- prometheus.MustNewConstMetric(c.refreshAge, prometheus.GaugeValue, refreshAge, slot)
	}
}

func sourceTimestampAge(now time.Time, at *time.Time) (float64, float64) {
	if at == nil {
		return 0, 0
	}
	const maximumAge = 365 * 24 * time.Hour
	return 1, max(0, min(now.Sub(*at), maximumAge).Seconds())
}

func validSourceStatuses(items []knowledge.SourceStatus) bool {
	const sourceSlots = 2
	if len(items) > sourceSlots {
		return false
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if (item.Slot != knowledge.AssistantQA && item.Slot != knowledge.AssistantAbout) || seen[item.Slot] ||
			item.Version < 0 {
			return false
		}
		seen[item.Slot] = true
		switch item.Status {
		case "ready", "stale", "unavailable":
		default:
			return false
		}
		switch item.ErrorCode {
		case "",
			"fetch_failed",
			"invalid_source",
			"source_limit",
			"source_timeout",
			"source_canceled",
			"source_unavailable":
		default:
			return false
		}
	}
	return true
}
