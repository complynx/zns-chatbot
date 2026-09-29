package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

func (b *Bot) bindPlanCommands(
	ctx context.Context,
	owner string,
	id int64,
	plan agent.Plan,
	input, request agent.Input,
	cached *interaction.SavedPlan,
) error {
	err := interaction.BindSavedRegistration(agenthost.CurrentRequestEvidence(input), id, plan, input, cached)
	if err != nil {
		return err
	}
	if plan.RegistrationAction != nil && plan.RegistrationAction.Name == agent.RegistrationAdminAssign {
		return nil
	}
	if plan.KnowledgeAction != nil {
		cached.KnowledgeCommand, err = b.knowledgeCoordinator().Bind(ctx, owner, plan.KnowledgeAction, input.Knowledge)
		if err != nil {
			return err
		}
	}
	if plan.OrderAction != nil {
		// Script writes update host-observed state. Keep the original utterance
		// and voice evidence while binding against those confirmed versions.
		request.Orders = input.Orders
		request.OrderCount = input.OrderCount
		request.EditableOrderCount = input.EditableOrderCount
		request.Extras = input.Extras
		cached.OrderCommand, err = (interaction.OrderCoordinator{Client: b.API, Store: interaction.Store{DB: b.DB}, EventID: b.currentOrderEvent()}).Bind(
			ctx,
			owner,
			plan.OrderAction,
			request,
			agenthost.CurrentRequestEvidence(request),
		)
	}
	return err
}
