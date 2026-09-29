package agenthost

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

// Provider freezes the owner-selected model settings and records model outcomes.
type Provider struct {
	Model          agent.Model
	SettingsReader interface {
		EffectiveModelSelection(context.Context, string) (modelsettings.Selection, error)
	}
	History interface {
		CheckHistoryGeneration(context.Context, string, int64) error
	}
}

func (p Provider) Settings(ctx context.Context, owner string) (context.Context, error) {
	selection, err := p.SettingsReader.EffectiveModelSelection(ctx, owner)
	if err != nil {
		return nil, err
	}
	return modelsettings.WithSelection(ctx, selection), nil
}

func (p Provider) CheckHistoryGeneration(ctx context.Context, owner string, generation int64) error {
	return p.History.CheckHistoryGeneration(ctx, owner, generation)
}

func (p Provider) Plan(ctx context.Context, input agent.Input) (agent.Plan, error) {
	ctx, span := observability.StartAgentEvent(
		ctx,
		observability.AgentEvent{Phase: "model", Operation: "model.loop"},
	)
	plan, err := p.Model.Plan(ctx, input)
	if err == nil {
		err = agent.Validate(plan)
		if err != nil {
			span.Outcome("invalid", "invalid_plan")
		}
	}
	span.Finish(err)
	return plan, err
}
