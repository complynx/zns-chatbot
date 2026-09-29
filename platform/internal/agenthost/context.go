package agenthost

import (
	"context"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

// AdmittedInput identifies one authenticated intake. Projection retains the
// original utterance and owner-bound AV references; it is not an authorization grant.
type AdmittedInput struct {
	Owner             string
	UpdateID          int64
	TrustedPartnerIDs []int64
	Projection        *agent.Input
}

// ContextSources supplies existing domain projections without transport or planning capabilities.
type ContextSources interface {
	Registration(context.Context, string, int64, []int64) (*agent.RegistrationContext, error)
	RefreshRegistration(context.Context, string, *agent.RegistrationContext) error
	Script(context.Context, string, int64) (*agent.ScriptContext, error)
}

type KnowledgeContextSource interface {
	Knowledge(context.Context, string, int64) (*agent.KnowledgeContext, error)
	RefreshKnowledge(context.Context, string, *agent.KnowledgeContext) error
}

type HistoryContextSource interface {
	History(context.Context, string, int64, *agent.Input, conversation.Window) error
	RefreshHistory(context.Context, string, *agent.Input) error
}

type UserReads interface {
	FoodView(context.Context, string, string, string) (legacyfood.View, error)
	BusinessCapabilities(context.Context, string, string) (agent.BusinessCapabilities, error)
}

type PendingFood interface {
	Read(context.Context, string) (legacyfood.Command, bool, error)
}

type AssetSource interface {
	Describe(context.Context) (agent.AssetContext, error)
}

// ContextBuilder owns assembly and live refresh order. Domain sources retain
// authorization; injected host readers retain reservations and never restore budgets.
// Sources, History, Knowledge, Reads, PendingFood and Projection must be non-nil.
// A nil Lineup is supported and produces unavailable timetable evidence.
type ContextBuilder struct {
	Sources     ContextSources
	History     HistoryContextSource
	Knowledge   KnowledgeContextSource
	Reads       UserReads
	PendingFood PendingFood
	Lineup      *agent.LineupSource
	OrderEvent  string
}

func (b ContextBuilder) AddSupporting(
	ctx context.Context, admitted AdmittedInput, assets AssetSource, window conversation.Window,
) error {
	input := admitted.Projection
	if err := b.addFoodHint(ctx, admitted.Owner, input); err != nil {
		return err
	}
	input.LineupSource = b.Lineup.Snapshot(time.Now())
	if err := addAssets(ctx, assets, input); err != nil {
		return err
	}
	var err error
	input.Knowledge, err = b.Knowledge.Knowledge(ctx, admitted.Owner, admitted.UpdateID)
	if err != nil {
		return err
	}
	input.Script, err = b.Sources.Script(ctx, admitted.Owner, admitted.UpdateID)
	if err != nil {
		return err
	}
	input.Registration, err = b.Sources.Registration(ctx, admitted.Owner, admitted.UpdateID, admitted.TrustedPartnerIDs)
	if err != nil {
		return err
	}
	return b.History.History(ctx, admitted.Owner, admitted.UpdateID, input, window)
}

// Refresh runs at every provider exposure, including provider-internal requests.
func (b ContextBuilder) Refresh(ctx context.Context, admitted AdmittedInput) error {
	input := admitted.Projection
	if err := b.addFoodHint(ctx, admitted.Owner, input); err != nil {
		return err
	}
	if input.Script != nil && input.Script.UpdateID != 0 {
		script, err := b.Sources.Script(ctx, admitted.Owner, input.Script.UpdateID)
		if err != nil {
			return err
		}
		input.Script = script
	}
	value, err := b.Reads.BusinessCapabilities(ctx, admitted.Owner, b.OrderEvent)
	if err != nil {
		return err
	}
	input.Business = &value
	if err = b.Knowledge.RefreshKnowledge(ctx, admitted.Owner, input.Knowledge); err != nil {
		return err
	}
	if err = b.Sources.RefreshRegistration(ctx, admitted.Owner, input.Registration); err != nil {
		return err
	}
	return b.History.RefreshHistory(ctx, admitted.Owner, input)
}
