package agenthost

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// ScriptCatalog supplies live domain capabilities. Registry ordering, fresh
// discovery and the running VM's immutable name scope belong to the host.
type ScriptCatalog interface {
	Capabilities(context.Context, string) (core.BusinessCapabilities, error)
	Workflow(bool) []ScriptToolEntry
	Memory() []ScriptToolEntry
	Profile() []ScriptToolEntry
	Reads() []ScriptToolEntry
	ProfileMutations(bool) []ScriptToolEntry
	Domains() []ScriptToolEntry
	Lineup() ScriptToolEntry
	Orders(core.BusinessCapabilities) []ScriptToolEntry
	Food(context.Context, string) ([]ScriptToolEntry, error)
	Privileged(context.Context, string) ([]ScriptToolEntry, error)
	Broadcast(context.Context, string) ([]ScriptToolEntry, error)
	Passes(context.Context, string) ([]ScriptToolEntry, error)
	Massage(context.Context, string) ([]ScriptToolEntry, error)
	Models(context.Context, string) ([]ScriptToolEntry, error)
	Credits(context.Context, string) ([]ScriptToolEntry, error)
	Knowledge(context.Context, string) ([]ScriptToolEntry, error)
}

const scriptDiagnosticPhase = "script"

type ScriptRegistry struct{ Catalog ScriptCatalog }

func (r ScriptRegistry) Entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	ctx, span := observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: scriptDiagnosticPhase, Operation: "script.registry.list"},
	)
	result, err := r.entries(ctx, owner)
	span.Finish(err)
	return result, err
}

func (r ScriptRegistry) entries(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	capabilities, err := r.Catalog.Capabilities(ctx, owner)
	if err != nil {
		return nil, scriptPublicError(err, errors.New("tool unavailable"))
	}
	profiles := r.baseEntries(capabilities)
	food, err := r.Catalog.Food(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, food...)
	privileged, err := r.Catalog.Privileged(ctx, owner)
	if err != nil {
		return nil, scriptPublicError(err, errors.New("tool unavailable"))
	}
	profiles = append(profiles, privileged...)
	broadcast, err := r.Catalog.Broadcast(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, broadcast...)
	passes, err := r.Catalog.Passes(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, passes...)
	massageEntries, err := r.Catalog.Massage(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, massageEntries...)
	models, err := r.Catalog.Models(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, models...)
	accounting, err := r.Catalog.Credits(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, accounting...)
	knowledgeEntries, err := r.Catalog.Knowledge(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, knowledgeEntries...)
	return restrictScriptRegistry(ctx, profiles), nil
}
func (r ScriptRegistry) Available(ctx context.Context, owner string) ([]scriptclient.Tool, error) {
	entries, err := r.Entries(ctx, owner)
	if err != nil {
		return nil, err
	}
	descriptors := make([]scriptclient.Tool, 0, len(entries))
	for _, entry := range entries {
		descriptors = append(descriptors, entry.Descriptor)
	}
	return descriptors, nil
}
func scriptBindings(tools []scriptclient.Tool) []scriptclient.Tool {
	bindings := make([]scriptclient.Tool, 0, len(tools))
	for _, tool := range tools {
		bindings = append(bindings, scriptclient.Tool{Name: tool.Name})
	}
	return bindings
}

type scriptRunScopeKey struct{}

// A running VM can call only its initial bindings. Live authorization may
// remove names; newly granted bindings become available in the next run.
func scriptRunScope(tools []scriptclient.Tool) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}
	return names
}

func restrictScriptRegistry(ctx context.Context, entries []ScriptToolEntry) []ScriptToolEntry {
	names, scoped := ctx.Value(scriptRunScopeKey{}).(map[string]bool)
	if !scoped {
		return entries
	}
	return slices.DeleteFunc(entries, func(entry ScriptToolEntry) bool {
		return !names[entry.Descriptor.Name]
	})
}

// Resolve requires the original binding and its current descriptor. Common
// authentication stays fresh; unrelated domain capability queries are avoided.
func (r ScriptRegistry) Resolve(ctx context.Context, owner, name string) (ScriptToolEntry, error) {
	ctx, span := observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: scriptDiagnosticPhase, Operation: "script.registry.resolve"},
	)
	result, err := r.resolve(ctx, owner, name)
	span.Finish(err)
	return result, err
}

func (r ScriptRegistry) resolve(ctx context.Context, owner, name string) (ScriptToolEntry, error) {
	if names, scoped := ctx.Value(scriptRunScopeKey{}).(map[string]bool); scoped && !names[name] {
		return ScriptToolEntry{}, errors.New("tool unavailable")
	}
	capabilities, err := r.Catalog.Capabilities(ctx, owner)
	if err != nil {
		return ScriptToolEntry{}, scriptPublicError(err, errors.New("tool unavailable"))
	}
	if entry, found := findScriptEntry(r.baseEntries(capabilities), name); found {
		return entry, nil
	}
	load := r.family(name)
	if load == nil {
		return ScriptToolEntry{}, errors.New("tool unavailable")
	}
	entries, err := load(ctx, owner)
	if err != nil {
		return ScriptToolEntry{}, scriptPublicError(err, errors.New("tool unavailable"))
	}
	if entry, found := findScriptEntry(entries, name); found {
		return entry, nil
	}
	return ScriptToolEntry{}, errors.New("tool unavailable")
}

func (r ScriptRegistry) baseEntries(capabilities core.BusinessCapabilities) []ScriptToolEntry {
	entries := append(r.Catalog.Workflow(capabilities.CanBook), r.Catalog.Memory()...)
	entries = append(entries, r.Catalog.Profile()...)
	entries = append(entries, r.Catalog.Reads()...)
	entries = append(entries, r.Catalog.ProfileMutations(capabilities.CanBook)...)
	entries = append(entries, r.Catalog.Domains()...)
	entries = append(entries, r.Catalog.Lineup())
	return append(entries, r.Catalog.Orders(capabilities)...)
}

func findScriptEntry(entries []ScriptToolEntry, name string) (ScriptToolEntry, bool) {
	for _, entry := range entries {
		if entry.Descriptor.Name == name {
			return entry, true
		}
	}
	return ScriptToolEntry{}, false
}

func (r ScriptRegistry) family(name string) func(context.Context, string) ([]ScriptToolEntry, error) {
	switch name {
	case "privileges.events", "passes.payments.queue", "passes.payments.history",
		"massage.practitioner.schedule", "massage.practitioner.preferences", "massage.practitioner.bookings":
		return r.Catalog.Privileged
	}
	namespace, _, _ := strings.Cut(name, ".")
	switch namespace {
	case "food":
		return r.Catalog.Food
	case "broadcasts":
		return r.Catalog.Broadcast
	case "passes":
		return r.Catalog.Passes
	case "massage":
		return r.Catalog.Massage
	case "models":
		return r.Catalog.Models
	case "credits":
		return r.Catalog.Credits
	case "knowledge":
		return r.Catalog.Knowledge
	default:
		return nil
	}
}
