package observability

import (
	"context"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type IdentityCacheRole string

const (
	IdentityCacheAPI IdentityCacheRole = "api"
	IdentityCacheBot IdentityCacheRole = "bot"
)

// RegisterIdentityCaches observes one existing enabled adapter. The runtime has
// separate API and bot adapters; register once per role, never for sandbox auth.
func (r *Runtime) RegisterIdentityCaches(role IdentityCacheRole, adapter *identity.Zitadel) error {
	if adapter == nil || (role != IdentityCacheAPI && role != IdentityCacheBot) {
		return errors.New("invalid identity cache observation configuration")
	}
	return r.Registry.Register(newIdentityCacheCollector(role, adapter.CacheObservations))
}

type identityCacheCollector struct {
	read      func(context.Context) (identity.CacheObservations, error)
	available *prometheus.Desc
	entries   *prometheus.Desc
	flights   *prometheus.Desc
	capacity  *prometheus.Desc
}

func newIdentityCacheCollector(
	role IdentityCacheRole,
	read func(context.Context) (identity.CacheObservations, error),
) *identityCacheCollector {
	desc := func(name, help string, labels ...string) *prometheus.Desc {
		return prometheus.NewDesc("zns_identity_cache_"+name, help, labels, prometheus.Labels{"adapter": string(role)})
	}
	return &identityCacheCollector{
		read:      read,
		available: desc("snapshot_available", "Whether both credential caches were read without contention."),
		entries: desc(
			"entries",
			"Stored credential counts; expired entries cannot authorize requests.",
			"cache",
			"state",
		),
		flights:  desc("flights", "Provider lookups still owned, including retired in-flight work.", "cache"),
		capacity: desc("capacity", "Configured maximum combined stored entries and flights per cache.", "cache"),
	}
}

func (c *identityCacheCollector) Describe(out chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{c.available, c.entries, c.flights, c.capacity} {
		out <- desc
	}
}

func (c *identityCacheCollector) Collect(out chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	item, err := c.read(ctx)
	if err != nil || !validIdentityCacheObservation(item) {
		out <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 0)
		return
	}
	out <- prometheus.MustNewConstMetric(c.available, prometheus.GaugeValue, 1)
	for index, value := range []identity.CacheObservation{item.Exchange, item.Introspection} {
		name := []string{"exchange", "introspection"}[index]
		out <- prometheus.MustNewConstMetric(c.entries, prometheus.GaugeValue, float64(value.Usable), name, "usable")
		out <- prometheus.MustNewConstMetric(c.entries, prometheus.GaugeValue, float64(value.Expired), name, "expired")
		out <- prometheus.MustNewConstMetric(c.flights, prometheus.GaugeValue, float64(value.Flights), name)
		out <- prometheus.MustNewConstMetric(c.capacity, prometheus.GaugeValue, float64(item.Capacity), name)
	}
}

func validIdentityCacheObservation(item identity.CacheObservations) bool {
	if item.Capacity <= 0 || item.Capacity > 1<<20 {
		return false
	}
	for _, value := range []identity.CacheObservation{item.Exchange, item.Introspection} {
		if value.Usable < 0 || value.Expired < 0 || value.Flights < 0 || value.Usable > item.Capacity ||
			value.Expired > item.Capacity || value.Flights > item.Capacity || value.Usable+value.Expired+value.Flights > item.Capacity {
			return false
		}
	}
	return true
}
