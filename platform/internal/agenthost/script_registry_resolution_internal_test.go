package agenthost

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type countingScriptCatalog struct {
	liveScriptCatalog

	counts  map[string]int
	latency time.Duration
	book    bool
	extra   bool
}

func (c *countingScriptCatalog) Capabilities(context.Context, string) (core.BusinessCapabilities, error) {
	c.counts["business"]++
	time.Sleep(c.latency)
	return core.BusinessCapabilities{CanBook: c.book}, nil
}

func (c *countingScriptCatalog) Orders(capability core.BusinessCapabilities) []ScriptToolEntry {
	if !capability.CanBook {
		return nil
	}
	entries := []ScriptToolEntry{{Descriptor: scriptclient.Tool{Name: "orders.choice"}}}
	if c.extra {
		entries = append(entries, ScriptToolEntry{Descriptor: scriptclient.Tool{Name: "orders.review.read"}})
	}
	return entries
}

func (c *countingScriptCatalog) familyEntries(family, name string) ([]ScriptToolEntry, error) {
	c.counts[family]++
	time.Sleep(c.latency)
	return []ScriptToolEntry{{Descriptor: scriptclient.Tool{Name: name}}}, nil
}

func (c *countingScriptCatalog) Food(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("food", "food.view")
}

func (c *countingScriptCatalog) Privileged(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("privileged", "passes.payments.queue")
}

func (c *countingScriptCatalog) Broadcast(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("broadcast", "broadcasts.preview")
}

func (c *countingScriptCatalog) Passes(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("passes", "passes.registration.read")
}

func (c *countingScriptCatalog) Massage(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("massage", "massage.book")
}

func (c *countingScriptCatalog) Models(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("models", "models.effective")
}

func (c *countingScriptCatalog) Credits(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("credits", "credits.usage")
}

func (c *countingScriptCatalog) Knowledge(context.Context, string) ([]ScriptToolEntry, error) {
	return c.familyEntries("knowledge", "knowledge.read")
}

func TestScriptResolutionQueriesOnlyRequestedFamily(t *testing.T) {
	t.Parallel()
	catalog := &countingScriptCatalog{counts: map[string]int{}, book: true, latency: 2 * time.Millisecond}
	registry := ScriptRegistry{Catalog: catalog}
	start := time.Now()
	tools, err := registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	fullDuration := time.Since(start)
	require.Len(t, catalog.counts, 9)
	scope := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	families := map[string]string{
		"orders.choice":            "business",
		"lineup.query":             "business",
		"food.view":                "food",
		"passes.payments.queue":    "privileged",
		"broadcasts.preview":       "broadcast",
		"passes.registration.read": "passes",
		"massage.book":             "massage",
		"models.effective":         "models",
		"credits.usage":            "credits",
		"knowledge.read":           "knowledge",
	}
	for _, tool := range tools {
		catalog.counts = map[string]int{}
		start = time.Now()
		entry, resolveErr := registry.Resolve(scope, "owner", tool.Name)
		require.NoError(t, resolveErr)
		require.Equal(t, tool.Name, entry.Descriptor.Name)
		expected := map[string]int{"business": 1}
		family, known := families[tool.Name]
		require.True(t, known, "each descriptor must have an exact resolution owner")
		expected[family] = 1
		require.Equal(t, expected, catalog.counts)
		t.Logf(
			"synthetic catalog=%s resolve=%s capability_calls=%d",
			fullDuration,
			time.Since(start),
			len(catalog.counts),
		)
	}
}

func TestScriptResolutionChecksCurrentRightsAndInitialScope(t *testing.T) {
	t.Parallel()
	catalog := &countingScriptCatalog{counts: map[string]int{}, book: true}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: catalog}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	catalog.book = false
	_, err = host.Registry.Resolve(ctx, "owner", "orders.choice")
	require.Error(t, err)
	_, err = host.discover(
		ctx,
		"owner",
		scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"orders.choice"}`)},
	)
	require.Error(t, err)
	catalog.book = true
	catalog.extra = true
	catalog.counts = map[string]int{}
	_, err = host.Registry.Resolve(ctx, "owner", "orders.review.read")
	require.Error(t, err)
	require.Empty(t, catalog.counts, "names outside initial scope never reach capability endpoints")
	_, err = host.discover(
		ctx,
		"owner",
		scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"orders.review.read"}`)},
	)
	require.Error(t, err)
	help, err := host.discover(
		ctx,
		"owner",
		scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"orders.choice"}`)},
	)
	require.NoError(t, err)
	require.Contains(t, string(help), "orders.choice")
	require.Equal(t, map[string]int{"business": 1}, catalog.counts)
	catalog.counts = map[string]int{}
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Len(t, catalog.counts, 9, "full list retains all fresh capability checks")
	require.NotContains(t, string(list), "orders.review.read")
	next, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	require.Len(t, next, len(tools)+1, "new grants enter the next binding scope")
}
