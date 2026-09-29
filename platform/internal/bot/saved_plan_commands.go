package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"
)

func (b *Bot) executePlannedRegistration(ctx context.Context, in incoming, id int64,
	plan interaction.SavedPlan) (string, error) {
	source, err := savedPlanSource(plan)
	if err != nil {
		return "", err
	}
	command := *plan.RegistrationCommand
	if command.Key == "" {
		command.Key = "tg-registration-" + strconv.FormatInt(id, 10)
	}
	_, err = (interaction.RegistrationExecutor{Derived: b.Host, Writer: b.registrationExecutionWriter(id)}).
		Command(ctx, in.owner, command, &source)
	return b.registrationExecutionNotice(ctx, in, err)
}

func (b *Bot) executePlannedAssignment(ctx context.Context, in incoming, id int64,
	plan interaction.SavedPlan) (string, error) {
	source, err := savedPlanSource(plan)
	if err != nil {
		return "", err
	}
	command := *plan.RegistrationAssignment
	if command.Key == "" {
		command.Key = "tg-admin-assignment-" + strconv.FormatInt(id, 10)
	}
	_, err = (interaction.RegistrationExecutor{Derived: b.Host, Writer: b.registrationExecutionWriter(id)}).
		Assignment(ctx, in.owner, command, &source)
	return b.registrationExecutionNotice(ctx, in, err)
}

func (b *Bot) executePlannedProfile(ctx context.Context, in incoming, id int64,
	plan interaction.SavedPlan) (string, error) {
	source, err := savedPlanSource(plan)
	if err != nil {
		return "", err
	}
	return b.executeProfileWithSource(ctx, in, id, *plan.ProfileCommand, &source)
}

func (b *Bot) executePlannedOrder(ctx context.Context, in incoming, id int64,
	plan interaction.SavedPlan) (string, error) {
	source, err := savedPlanSource(plan)
	if err != nil {
		return "", err
	}
	return b.executeOrderWithSource(ctx, in.owner, id, *plan.OrderCommand, &source)
}

func (b *Bot) executePlannedWorkflow(ctx context.Context, in incoming, id int64,
	plan interaction.SavedPlan) (string, error) {
	source, err := savedPlanSource(plan)
	if err != nil {
		return "", err
	}
	// A proposal is not a completed action; the executor supplies the outcome.
	action := plan.Plan.Action
	command := workflow.Action{Name: action.Name, SlotID: action.SlotID, Version: plan.Version,
		Key: "tg-" + strconv.FormatInt(id, 10), Origin: originAgent}
	workflow, err := b.Host.ExecuteDerivedWorkflow(ctx, in.owner, command, source)
	return b.workflowOutcome(ctx, in.owner, id, command, workflow, err)
}
