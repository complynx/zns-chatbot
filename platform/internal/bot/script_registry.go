package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const maxOrdinaryScriptResult = 2048

type scriptToolEntry struct {
	descriptor  scriptclient.Tool
	prepare     func(context.Context, string, int64, scriptclient.ToolCall, agent.Input) (scriptToolRecord, error)
	execute     func(context.Context, string, scriptclient.ToolCall, scriptToolRecord, *agent.Input) (any, error)
	resultLimit int
}

// Discovery and dispatch share the same current, authenticated registry.
// Domain preparers retain authority binding and durable command construction.
func (b *Bot) authorizedScriptRegistry(ctx context.Context, owner string) ([]scriptToolEntry, error) {
	capabilities, err := b.API.BusinessCapabilities(ctx, owner, b.currentOrderEvent())
	if err != nil {
		return nil, errors.New("tool unavailable")
	}
	workflow := scriptTools(capabilities.CanBook)
	memory := memoryTools()
	profiles := append(b.scriptProfileEntries(), b.scriptReadEntries()...)
	profiles = append(profiles, b.scriptProfileMutationEntries(capabilities.CanBook)...)
	profiles = append(profiles, b.scriptDomainEntries()...)
	profiles = append(profiles, b.scriptLineupEntry())
	profiles = append(profiles, b.scriptModernOrderEntries(capabilities)...)
	food, err := b.scriptFoodEntries(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, food...)
	privileged, err := b.scriptPrivilegedReadEntries(ctx, owner)
	if err != nil {
		return nil, errors.New("tool unavailable")
	}
	profiles = append(profiles, privileged...)
	broadcast, err := b.scriptBroadcastEntries(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, broadcast...)
	passes, err := b.scriptPassEntries(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, passes...)
	massageEntries, err := b.scriptMassageEntries(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, massageEntries...)
	models, err := b.scriptModelEntries(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, models...)
	accounting, err := b.scriptCreditEntries(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, accounting...)
	knowledgeEntries, err := b.scriptKnowledgeEntries(ctx, owner)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, knowledgeEntries...)
	entries := make([]scriptToolEntry, 0, len(workflow)+len(memory)+len(profiles))
	for _, descriptor := range workflow {
		entries = append(
			entries,
			scriptToolEntry{
				descriptor:  descriptor,
				prepare:     b.prepareWorkflowOrderTool,
				execute:     b.executeWorkflowOrderTool,
				resultLimit: maxOrdinaryScriptResult,
			},
		)
	}
	for _, descriptor := range memory {
		entries = append(
			entries,
			scriptToolEntry{
				descriptor:  descriptor,
				prepare:     b.prepareScriptMemoryTool,
				execute:     b.executeScriptMemoryTool,
				resultLimit: maxMemoryToolBytes,
			},
		)
	}
	return restrictScriptRegistry(ctx, append(entries, profiles...)), nil
}

func (b *Bot) availableScriptTools(ctx context.Context, owner string) ([]scriptclient.Tool, error) {
	entries, err := b.authorizedScriptRegistry(ctx, owner)
	if err != nil {
		return nil, err
	}
	descriptors := make([]scriptclient.Tool, 0, len(entries))
	for _, entry := range entries {
		descriptors = append(descriptors, entry.descriptor)
	}
	return descriptors, nil
}

func (b *Bot) prepareScriptMemoryTool(
	ctx context.Context,
	owner string,
	updateID int64,
	call scriptclient.ToolCall,
	_ agent.Input,
) (scriptToolRecord, error) {
	record := scriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name, Error: scriptInterrupted}}
	return b.prepareMemoryTool(ctx, owner, updateID, call, record)
}

func (b *Bot) executeScriptMemoryTool(
	ctx context.Context,
	owner string,
	call scriptclient.ToolCall,
	record scriptToolRecord,
	_ *agent.Input,
) (any, error) {
	return b.executeMemoryTool(ctx, owner, call, record)
}

// The worker only needs names. Full schemas remain host-side until live help.
func scriptBindings(tools []scriptclient.Tool) []scriptclient.Tool {
	bindings := make([]scriptclient.Tool, 0, len(tools))
	for _, tool := range tools {
		bindings = append(bindings, scriptclient.Tool{Name: tool.Name})
	}
	return bindings
}

// Intermediate reads stay in owner-private receipts; the script chooses the
// bounded evidence to return to the next model prompt.
func scriptCallProjection(outcome agent.ScriptToolResult) agent.ScriptToolResult {
	switch outcome.Name {
	case scriptLineupQuery, scriptKnowledgeRead,
		scriptKnowledgeProposals,
		scriptKnowledgeReviewQueue,
		scriptKnowledgeMemos,
		"knowledge.memo_read",
		scriptPassRead,
		scriptPassAdminRead,
		scriptPassAdminTarget,
		scriptPassReviewRead,
		scriptPassTakeoverRead,
		scriptPassTiers,
		scriptHistoryPageToolName,
		scriptHistoryRead,
		modernOrdersEvents, modernOrdersEvent, modernOrdersBrowse, modernOrdersContacts, modernOrdersHistory, modernOrdersHistoryRead,
		modernOrdersChoice, modernOrdersInspect, modernOrdersInstructions, modernOrdersQuote, modernOrdersInbox, modernOrdersReviewRead,
		scriptFoodView,
		scriptOrdersPageToolName,
		"orders.read",
		scriptPassEvents,
		scriptPassGet,
		scriptPassInvitations,
		scriptPassEventRead,
		scriptMassageParties,
		scriptMassageSlots,
		scriptMassageBookings,
		scriptMassageProviderRead,
		scriptBroadcastAudience, scriptBroadcastProfile, scriptBroadcastReview,
		scriptPrivilegeEvents, scriptPaymentQueue, scriptPaymentHistory, scriptPractitionerSchedule, scriptPractitionerPreferences, scriptPractitionerBookings:
		if outcome.Error == "" {
			outcome.Result = json.RawMessage(
				`{"read_completed":true,"payload_omitted":true,"evidence":"See script result; omission is not absence."}`,
			)
		}
		return outcome
	default:
		return memoryCallProjection(outcome)
	}
}
