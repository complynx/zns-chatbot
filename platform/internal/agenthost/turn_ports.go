package agenthost

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

// TurnInput is copied from authenticated intake. Text is the original utterance;
// MediaID identifies owner-bound intake, not speech from inspected recordings.
type TurnInput struct {
	Owner             string
	UpdateID          int64
	Text              string
	MediaID           string
	TrustedPartnerIDs []int64
}

type TurnState interface {
	ValidateReply(context.Context, string, int64) error
	ValidateHistory(context.Context, string, int64, int64) error
	ValidatePlan(context.Context, string, int64, interaction.SavedPlan) error
	ClearAV(context.Context, string, []string) error
}

type QuestionBudget interface {
	ReserveQuestion(context.Context, string, int64) (bool, int, error)
	QuotaNotice(context.Context, string) (interaction.SavedPlan, error)
}

type InitialContext interface {
	Current(context.Context, string) (workflow.Workflow, error)
	Catalog(context.Context, string) ([]workflow.Slot, error)
	CurrentAV(context.Context, *agent.Input) error
	Orders(context.Context, string, *agent.Input) error
	Profile(context.Context, string, *agent.Input) error
	Media(context.Context, *agent.Input) error
}

type ModelProvider interface {
	Settings(context.Context, string) (context.Context, error)
	Plan(context.Context, agent.Input) (agent.Plan, error)
	CheckHistoryGeneration(context.Context, string, int64) error
}

// ReadTools adapts domain-specific reads and the script host.
// History and knowledge policy are injected separately as host owners.
type ReadTools interface {
	Lineup(agent.LineupQuery, *agent.Input) error
	Registration(context.Context, string, int64, agent.RegistrationProposal, *agent.Input) error
	Script(context.Context, string, int64, agent.ScriptProposal, *agent.Input) error
}

type TurnHistory interface {
	InitialHistory(context.Context, string) (conversation.Window, error)
	ReadHistory(context.Context, string, int64, agent.HistoryProposal, *agent.Input) error
}

type TurnKnowledge interface {
	ReadKnowledge(context.Context, string, int64, agent.KnowledgeProposal, *agent.Input) error
}

type AVInspection interface {
	Inspection(context.Context, string, int64) (*agent.AVInspectionContext, error)
	Refine(context.Context, string, int64, agent.MediaProposal, *agent.Input) (i18n.ID, error)
}

// Exposure keeps source capture atomic with live context refresh. It never calls
// the model or selects a saved winner.
type Exposure interface {
	Reset()
	Expose(context.Context, *agent.Input) error
	Snapshot() *interaction.PlanAuthority
	Validate(context.Context, interaction.SavedPlan) error
}

type PlanBinding interface {
	Commands(context.Context, string, agent.Plan, agent.Input, agent.Input, *interaction.SavedPlan) error
	MediaSelection(*interaction.SavedPlan, agent.Input, agent.Input)
	Failure(error) (notice i18n.ID, propagate bool)
}
