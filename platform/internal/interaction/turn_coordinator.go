package interaction

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
)

// TurnHost supplies model/history work for a single admitted input. It does not
// control persistence, winner selection, replay order or terminal retry policy.
type TurnHost interface {
	Prepare(context.Context) (conversation.Window, error)
	Plan(context.Context, conversation.Window) (SavedPlan, error)
	Validate(context.Context, SavedPlan) error
	ClearAV(context.Context, []string) error
}

type TurnCoordinator struct{ Store Store }

// ResumeOrPlan validates both restored and competing winners before releasing
// transient AV data. Cleanup failures leave the saved winner available to retry.
func (c TurnCoordinator) ResumeOrPlan(
	ctx context.Context,
	owner string,
	updateID int64,
	host TurnHost,
) (SavedPlan, bool, error) {
	plan, err := c.Store.Load(ctx, owner, updateID)
	if err == nil {
		if err = host.Validate(ctx, plan); err != nil {
			return SavedPlan{}, true, err
		}
		return plan, true, host.ClearAV(ctx, plan.AVIDs)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return plan, false, err
	}
	window, err := host.Prepare(ctx)
	if err != nil {
		return SavedPlan{}, false, err
	}
	plan, err = host.Plan(ctx, window)
	if err != nil {
		return plan, false, err
	}
	plan.HistoryGeneration = window.Generation
	bindOrderGeneration(&plan, window.Generation)
	plan.BindKind()
	if err = host.Validate(ctx, plan); err != nil {
		return SavedPlan{}, false, err
	}
	plan, err = c.Store.SaveWinner(ctx, owner, updateID, plan)
	if err == nil {
		err = host.Validate(ctx, plan)
	}
	if err == nil {
		err = host.ClearAV(ctx, plan.AVIDs)
	}
	return plan, false, err
}

// Bind only new host-produced commands. Replays must retain the exact command
// and generation used for the durable winner and domain receipt hash.
func bindOrderGeneration(plan *SavedPlan, generation int64) {
	if plan.OrderCommand == nil || plan.OrderCommand.Origin != orderOriginAgent ||
		(plan.OrderCommand.Name != orderActionCreate && plan.OrderCommand.Name != orderActionEdit) {
		return
	}
	command := *plan.OrderCommand
	command.HistoryGeneration = &generation
	plan.OrderCommand = &command
}
