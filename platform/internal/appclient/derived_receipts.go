package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/workflow"
)

func (c Host) OrderReceipt(
	ctx context.Context,
	owner string,
	command orders.Command,
	source readsource.Derivation,
) (derivedmutation.Receipt[orders.Order], error) {
	var result derivedmutation.Receipt[orders.Order]
	if err := validateDerivedCommandLimit(command, source, maxDerivedOrderCommandBytes); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) (derivedmutation.Receipt[orders.Order], error) {
					return s.OrderReceipt(ctx, actor, command)
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/order-actions/receipt", command, source, &result)
	return result, err
}

func (c Host) WorkflowReceipt(
	ctx context.Context,
	owner string,
	command workflow.Action,
	source readsource.Derivation,
) (derivedmutation.Receipt[workflow.Workflow], error) {
	var result derivedmutation.Receipt[workflow.Workflow]
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) (derivedmutation.Receipt[workflow.Workflow], error) {
					return s.WorkflowReceipt(ctx, actor, command)
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/actions/receipt", command, source, &result)
	return result, err
}

func (c Host) PassBookingReceipt(
	ctx context.Context,
	owner string,
	command passbooking.Command,
	source readsource.Derivation,
) (derivedmutation.Receipt[passbooking.Booking], error) {
	var result derivedmutation.Receipt[passbooking.Booking]
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) (derivedmutation.Receipt[passbooking.Booking], error) {
					return s.PassBookingReceipt(ctx, actor, command)
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-actions/receipt", command, source, &result)
	return registrationHTTPResult(result, err)
}

func (c Host) PassAssignmentReceipt(
	ctx context.Context,
	owner string,
	command passbooking.AdminAssignment,
	source readsource.Derivation,
) (derivedmutation.Receipt[passbooking.AdminAssignmentResult], error) {
	var result derivedmutation.Receipt[passbooking.AdminAssignmentResult]
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) (derivedmutation.Receipt[passbooking.AdminAssignmentResult], error) {
					return s.PassAssignmentReceipt(ctx, actor, command)
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-assignments/receipt", command, source, &result)
	return registrationHTTPResult(result, err)
}

func (c Host) PassProfileReceipt(
	ctx context.Context,
	owner string,
	command passes.Command,
	source readsource.Derivation,
) (derivedmutation.Receipt[passes.Profile], error) {
	var result derivedmutation.Receipt[passes.Profile]
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) (derivedmutation.Receipt[passes.Profile], error) {
					return s.PassProfileReceipt(ctx, actor, command)
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-profiles/receipt", command, source, &result)
	return registrationHTTPResult(result, err)
}

func (c Host) KnowledgeReceipt(ctx context.Context, owner string, command knowledge.Command,
	source readsource.Derivation) (derivedmutation.Receipt[knowledge.Result], error) {
	var result derivedmutation.Receipt[knowledge.Result]
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	err := c.derivedRequest(ctx, owner, "/internal/knowledge/derived/receipt", command, source, &result)
	return result, err
}
