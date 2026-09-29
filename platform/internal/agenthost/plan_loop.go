package agenthost

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

func (h *Turn) planWithAV(ctx context.Context, input *agent.Input) (agent.Plan, []string, error) {
	ctx, settingsErr := h.Model.Settings(ctx, h.Input.Owner)
	if settingsErr != nil {
		return agent.Plan{}, nil, settingsErr
	}
	inspection, inspectionErr := h.AV.Inspection(ctx, h.Input.Owner, h.Input.UpdateID)
	if inspectionErr != nil {
		return agent.Plan{}, nil, inspectionErr
	}
	input.AVInspection = inspection
	var ids []string
	if input.AV != nil {
		ids = append(ids, input.AV.ID)
	}
	for turn := 0; ; turn++ {
		modelContext := agent.WithRequestScope(
			ctx,
			agent.RequestScope{Owner: h.Input.Owner, UpdateID: h.Input.UpdateID, Turn: turn},
		)
		plan, err := h.fencedPlan(modelContext, input)
		if err != nil {
			return agent.Plan{}, ids, err
		}
		handled, err := h.performRead(ctx, plan, input)
		if err != nil {
			return agent.Plan{}, ids, err
		}
		if handled {
			continue
		}
		if plan.MediaAction == nil || plan.MediaAction.Intent != "inspect_video" {
			return plan, ids, nil
		}
		proposal := *plan.MediaAction
		notice, err := h.AV.Refine(ctx, h.Input.Owner, h.Input.UpdateID, proposal, input)
		if err != nil {
			return agent.Plan{}, ids, err
		}
		if notice != "" {
			text, translateErr := i18n.Translate(input.Language, notice, nil)
			return agent.Plan{View: agent.MediaView, Text: text}, ids, translateErr
		}
		ids = append(ids, proposal.MediaID)
	}
}

// One invocation captures every exposure from provider selection and retries.
func (h *Turn) fencedPlan(ctx context.Context, input *agent.Input) (agent.Plan, error) {
	h.Exposure.Reset()
	input.BeforeProvider = h.Exposure.Expose
	if err := h.Exposure.Expose(ctx, input); err != nil {
		return agent.Plan{}, err
	}
	plan, err := h.Model.Plan(ctx, *input)
	if err == nil {
		err = h.Model.CheckHistoryGeneration(ctx, h.Input.Owner, input.HistoryGeneration)
	}
	if err == nil && !readOnlyContextPlan(plan) {
		err = h.Exposure.Validate(ctx, interaction.SavedPlan{Kind: interaction.DerivedPlan,
			PassAuthority: h.Exposure.Snapshot(), HistoryGeneration: input.HistoryGeneration})
	}
	return plan, err
}

func readOnlyContextPlan(plan agent.Plan) bool {
	return plan.LineupAction != nil || plan.HistoryAction != nil ||
		(plan.RegistrationAction != nil && plan.RegistrationAction.Name == agent.RegistrationRead) ||
		(plan.ScriptAction == nil && agent.IsKnowledgeRead(plan.KnowledgeAction))
}

func (h *Turn) performRead(ctx context.Context, plan agent.Plan, input *agent.Input) (bool, error) {
	if plan.LineupAction != nil {
		return true, h.Reads.Lineup(*plan.LineupAction, input)
	}
	if p := plan.RegistrationAction; p != nil && p.Name == agent.RegistrationRead {
		return true, h.Reads.Registration(ctx, h.Input.Owner, h.Input.UpdateID, *p, input)
	}
	if plan.HistoryAction != nil {
		return true, h.History.ReadHistory(ctx, h.Input.Owner, h.Input.UpdateID, *plan.HistoryAction, input)
	}
	if plan.ScriptAction != nil {
		return true, h.Reads.Script(ctx, h.Input.Owner, h.Input.UpdateID, *plan.ScriptAction, input)
	}
	if agent.IsKnowledgeRead(plan.KnowledgeAction) {
		return true, h.Knowledge.ReadKnowledge(ctx, h.Input.Owner, h.Input.UpdateID, *plan.KnowledgeAction, input)
	}
	return false, nil
}
