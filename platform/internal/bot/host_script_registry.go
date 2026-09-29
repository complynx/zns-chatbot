package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type botScriptCatalog struct{ bot *Bot }

func (c botScriptCatalog) Capabilities(ctx context.Context, owner string) (core.BusinessCapabilities, error) {
	return c.bot.API.BusinessCapabilities(ctx, owner, c.bot.currentOrderEvent())
}
func (c botScriptCatalog) Workflow(canBook bool) []agenthost.ScriptToolEntry {
	tools := scriptTools(canBook)
	entries := make([]agenthost.ScriptToolEntry, 0, len(tools))
	for _, descriptor := range tools {
		entries = append(
			entries,
			agenthost.ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     c.bot.prepareWorkflowOrderTool,
				Execute:     c.bot.executeWorkflowOrderTool,
				ResultLimit: maxOrdinaryScriptResult,
			},
		)
	}
	return entries
}
func (c botScriptCatalog) Memory() []agenthost.ScriptToolEntry {
	tools := memoryTools()
	entries := make([]agenthost.ScriptToolEntry, 0, len(tools))
	for _, descriptor := range tools {
		entries = append(
			entries,
			agenthost.ScriptToolEntry{
				Descriptor:  descriptor,
				Prepare:     c.bot.prepareScriptMemoryTool,
				Execute:     c.bot.executeScriptMemoryTool,
				ResultLimit: maxMemoryToolBytes,
			},
		)
	}
	return entries
}
func (c botScriptCatalog) ProfileMutations(canBook bool) []agenthost.ScriptToolEntry {
	return c.bot.scriptProfileMutationEntries(canBook)
}

func (c botScriptCatalog) Profile() []agenthost.ScriptToolEntry { return c.bot.scriptProfileEntries() }

func (c botScriptCatalog) Knowledge(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptKnowledgeEntries(ctx, owner)
}

func (c botScriptCatalog) Broadcast(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptBroadcastEntries(ctx, owner)
}

func (c botScriptCatalog) Credits(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptCreditEntries(ctx, owner)
}

func (c botScriptCatalog) Lineup() agenthost.ScriptToolEntry { return c.bot.scriptLineupEntry() }

func (c botScriptCatalog) Privileged(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptPrivilegedReadEntries(ctx, owner)
}

func (c botScriptCatalog) Domains() []agenthost.ScriptToolEntry { return c.bot.scriptDomainEntries() }

func (c botScriptCatalog) Massage(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptMassageEntries(ctx, owner)
}

func (c botScriptCatalog) Reads() []agenthost.ScriptToolEntry { return c.bot.scriptReadEntries() }

func (c botScriptCatalog) Passes(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptPassEntries(ctx, owner)
}

func (c botScriptCatalog) Food(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptFoodEntries(ctx, owner)
}

func (c botScriptCatalog) Models(ctx context.Context, owner string) ([]agenthost.ScriptToolEntry, error) {
	return c.bot.scriptModelEntries(ctx, owner)
}

func (c botScriptCatalog) Orders(capabilities core.BusinessCapabilities) []agenthost.ScriptToolEntry {
	return c.bot.scriptModernOrderEntries(capabilities)
}
