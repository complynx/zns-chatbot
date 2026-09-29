package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func (b *Bot) bindPlanCommands(
	ctx context.Context,
	owner string,
	plan agent.Plan,
	input, request agent.Input,
	cached *cachedPlan,
) error {
	var err error
	if plan.RegistrationAction != nil && plan.RegistrationAction.Name == agent.RegistrationAdminAssign {
		cached.RegistrationAssignment, cached.RegistrationMenu, err = bindAdminAssignment(plan, input)
		return err
	}
	cached.RegistrationCommand, cached.RegistrationMenu, err = bindRegistrationPlan(plan, input)
	if err != nil {
		return err
	}
	if plan.KnowledgeAction != nil {
		cached.KnowledgeCommand, err = b.bindKnowledgeCommand(ctx, owner, plan.KnowledgeAction, input.Knowledge)
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
		cached.OrderCommand, err = b.proposedOrderCommand(ctx, owner, plan.OrderAction, request)
	}
	return err
}
