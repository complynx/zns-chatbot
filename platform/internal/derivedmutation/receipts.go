package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

// Receipt reports only a currently authorized exact-command receipt. The probe never applies a new effect.
type Receipt[T any] struct {
	Found  bool `json:"found"`
	Result T    `json:"result"`
}

func (s Service) OrderReceipt(
	ctx context.Context,
	actor string,
	command orders.Command,
) (Receipt[orders.Order], error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Receipt[orders.Order]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Orders.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return Receipt[orders.Order]{}, err
	}
	result, found := prepared.Replay()
	if !found {
		return Receipt[orders.Order]{}, nil
	}
	return Receipt[orders.Order]{Found: found, Result: result}, nil
}

func (s Service) WorkflowReceipt(
	ctx context.Context,
	actor string,
	command workflow.Action,
) (Receipt[workflow.Workflow], error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Receipt[workflow.Workflow]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Workflow.PrepareWorkflowInTx(ctx, tx, actor, command)
	if err != nil {
		return Receipt[workflow.Workflow]{}, err
	}
	result, found := prepared.Replay()
	if !found {
		return Receipt[workflow.Workflow]{}, nil
	}
	return Receipt[workflow.Workflow]{Found: found, Result: result}, nil
}

func (s Service) PassBookingReceipt(
	ctx context.Context,
	actor string,
	command passbooking.Command,
) (Receipt[passbooking.Booking], error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Receipt[passbooking.Booking]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Registration.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return Receipt[passbooking.Booking]{}, err
	}
	result, found := prepared.Replay()
	if !found {
		return Receipt[passbooking.Booking]{}, nil
	}
	return Receipt[passbooking.Booking]{Found: found, Result: result}, nil
}

func (s Service) PassAssignmentReceipt(
	ctx context.Context,
	actor string,
	command passbooking.AdminAssignment,
) (Receipt[passbooking.AdminAssignmentResult], error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Receipt[passbooking.AdminAssignmentResult]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Registration.PrepareAssignmentInTx(ctx, tx, actor, command)
	if err != nil {
		return Receipt[passbooking.AdminAssignmentResult]{}, err
	}
	result, found := prepared.Replay()
	if !found {
		return Receipt[passbooking.AdminAssignmentResult]{}, nil
	}
	return Receipt[passbooking.AdminAssignmentResult]{Found: found, Result: result}, nil
}

func (s Service) PassProfileReceipt(
	ctx context.Context,
	actor string,
	command passes.Command,
) (Receipt[passes.Profile], error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Receipt[passes.Profile]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	prepared, err := s.Profile.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return Receipt[passes.Profile]{}, err
	}
	result, found := prepared.Replay()
	if !found {
		return Receipt[passes.Profile]{}, nil
	}
	return Receipt[passes.Profile]{Found: found, Result: result}, nil
}
