package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (c Host) ExecuteDerivedPassBooking(
	ctx context.Context,
	owner string,
	command passbooking.Command,
	source readsource.Derivation,
) (passbooking.Booking, error) {
	var result passbooking.Booking
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if _, err := c.AdmitPassBooking(ctx, owner, command, &source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (passbooking.Booking, error) {
				return s.ExecutePassBooking(ctx, actor, command, source.Clone())
			}),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-actions", command, source, &result)
	return registrationHTTPResult(result, err)
}

func (c Host) AssignDerivedPass(
	ctx context.Context,
	owner string,
	command passbooking.AdminAssignment,
	source readsource.Derivation,
) (passbooking.AdminAssignmentResult, error) {
	var result passbooking.AdminAssignmentResult
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) (passbooking.AdminAssignmentResult, error) {
					return s.AssignPass(ctx, actor, command, source.Clone())
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-assignments", command, source, &result)
	return registrationHTTPResult(result, err)
}

func (c Host) ExecuteDerivedPassProfile(
	ctx context.Context,
	owner string,
	command passes.Command,
	source readsource.Derivation,
) (passes.Profile, error) {
	var result passes.Profile
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (passes.Profile, error) {
				return s.ExecutePassProfile(ctx, actor, command, source.Clone())
			}),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-profiles", command, source, &result)
	return registrationHTTPResult(result, err)
}

func (c Host) RunDerivedPassBatch(
	ctx context.Context,
	owner string,
	command passbooking.RuntimeBatch,
	source readsource.Derivation,
) ([]passbooking.RuntimeBatchItem, error) {
	var result []passbooking.RuntimeBatchItem
	if err := validateDerivedCommand(command, source); err != nil {
		return registrationHTTPResult(result, err)
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(
				ctx,
				c,
				owner,
				func(s derivedmutation.Service, actor string) ([]passbooking.RuntimeBatchItem, error) {
					return s.RunPassBatch(ctx, actor, command, source.Clone())
				},
			),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/pass-batches", command, source, &result)
	return registrationHTTPResult(result, err)
}
