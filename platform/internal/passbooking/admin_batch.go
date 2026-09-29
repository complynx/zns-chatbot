package passbooking

import (
	"context"
	"errors"
	"fmt"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// AdminAssignBatch validates the whole envelope before processing recipients in
// order. Each item reuses AdminAssign's authorization, transaction and replay key.
// Domain rejections do not stop later items. Infrastructure errors stop work and
// return all outcomes, including unattempted recipients, with a non-nil error.
func (s Service) AdminAssignBatch(
	ctx context.Context,
	actor string,
	commands []AdminAssignment,
) ([]AdminBatchOutcome, error) {
	if len(commands) == 0 || len(commands) > MaxAdminBatchRecipients {
		return nil, invalid()
	}
	items := make([]AdminBatchOutcome, len(commands))
	events := make([]string, len(commands))
	for i, command := range commands {
		if err := validateAdminAssignment(command); err != nil {
			return nil, err
		}
		items[i] = AdminBatchOutcome{Target: command.Target, Key: command.Key, Status: AdminBatchNotAttempted}
		events[i] = command.Event
	}
	if err := validateAdminBatch(items, events); err != nil {
		return nil, err
	}
	for i, command := range commands {
		if err := ctx.Err(); err != nil {
			return items, err
		}
		result, err := s.AdminAssign(ctx, actor, command)
		if stop := adminBatchResult(&items[i], err); stop != nil {
			return items, stop
		}
		if err == nil {
			items[i].Assignment = &result
		}
	}
	return items, nil
}

// AdminCancelBatch retains Execute's global/event-payment administrator ACL and cancellation
// semantics, including transactional partner updates and queue recalculation.
func (s Service) AdminCancelBatch(
	ctx context.Context,
	actor string,
	commands []AdminCancellation,
) ([]AdminBatchOutcome, error) {
	const maxKey = 200
	if len(commands) == 0 || len(commands) > MaxAdminBatchRecipients {
		return nil, invalid()
	}
	items := make([]AdminBatchOutcome, len(commands))
	events := make([]string, len(commands))
	for i, command := range commands {
		if !boundedText(command.Target, maxKey, false) || !boundedText(command.Event, maxKey, false) ||
			!boundedText(command.Key, maxKey, false) {
			return nil, invalid()
		}
		if err := validate(command.command()); err != nil {
			return nil, err
		}
		items[i] = AdminBatchOutcome{Target: command.Target, Key: command.Key, Status: AdminBatchNotAttempted}
		events[i] = command.Event
	}
	if err := validateAdminBatch(items, events); err != nil {
		return nil, err
	}
	for i, command := range commands {
		if err := ctx.Err(); err != nil {
			return items, err
		}
		_, err := s.Execute(ctx, actor, command.command())
		if stop := adminBatchResult(&items[i], err); stop != nil {
			return items, stop
		}
	}
	return items, nil
}

func adminBatchResult(item *AdminBatchOutcome, err error) error {
	if err == nil {
		item.Status = AdminBatchSucceeded
		return nil
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok {
		item.Status, item.Code = AdminBatchRejected, problem.Code
		return nil
	}
	item.Status = AdminBatchInterrupted
	return fmt.Errorf("pass admin batch interrupted: %w", err)
}
