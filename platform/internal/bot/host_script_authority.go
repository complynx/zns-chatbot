package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type botScriptAuthority struct{ bot *Bot }

func (p botScriptAuthority) Generation(ctx context.Context, owner string) (int64, error) {
	return p.bot.API.HistoryGeneration(ctx, owner)
}
func (p botScriptAuthority) MemoryState(ctx context.Context, owner string) (knowledge.MemoryDeletionState, error) {
	return p.bot.API.MemoryDeletions(ctx, owner)
}

func (p botScriptAuthority) Registration(ctx context.Context, owner string, read *agent.RegistrationReadResult) error {
	return p.bot.registrationRevalidator().Read(ctx, owner, read)
}

func (p botScriptAuthority) SourcesChanged(
	ctx context.Context,
	owner string,
	refs []readsource.Authority,
) (bool, error) {
	return p.bot.readAuthoritiesChanged(ctx, owner, refs)
}

func (p botScriptAuthority) RegistrationContextChanged(
	ctx context.Context,
	owner string,
	dependency interaction.PassContextDependency,
) (bool, error) {
	return p.bot.registrationRevalidator().DependencyChanged(ctx, owner, dependency)
}

func (p botScriptAuthority) RegistrationCallChanged(
	ctx context.Context,
	owner string,
	call agenthost.ScriptToolRecord,
) (bool, error) {
	return p.bot.scriptPassCallChanged(ctx, owner, call)
}

func (p botScriptAuthority) RegistrationSummaryChanged(
	ctx context.Context,
	owner string,
	outcome agent.ScriptToolResult,
) (bool, error) {
	return p.bot.passOperationSummaryChanged(ctx, owner, outcome)
}

func (p botScriptAuthority) PassBookingReceipt(ctx context.Context, owner string, command passbooking.Command,
	source readsource.Derivation) (derivedmutation.Receipt[passbooking.Booking], error) {
	return p.bot.Host.PassBookingReceipt(ctx, owner, command, source)
}

func (p botScriptAuthority) KnowledgeReceipt(
	ctx context.Context,
	owner string,
	command knowledge.Command,
	source readsource.Derivation,
) (derivedmutation.Receipt[knowledge.Result], error) {
	return p.bot.Host.KnowledgeReceipt(ctx, owner, command, source)
}
