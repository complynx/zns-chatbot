package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func directRegistration[T any](ctx context.Context, c Client, owner string,
	operation func(passbooking.Service, string) (T, error),
) (T, error) {
	var zero T
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return zero, err
	}
	value, err := operation(c.LocalRegistration.Service, actor)
	if err != nil {
		return zero, orderApplicationError(err)
	}
	return value, nil
}

func (c Client) localPassCapabilities(ctx context.Context, owner, event string) (passbooking.Capabilities, error) {
	return directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (passbooking.Capabilities, error) {
			return service.Capabilities(ctx, actor, event)
		},
	)
}

func (c Client) localPassAdminTarget(
	ctx context.Context,
	owner, event string,
	id int64,
) (passbooking.AdminTarget, error) {
	return directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (passbooking.AdminTarget, error) {
			if id <= 0 {
				return passbooking.AdminTarget{}, &core.ProblemError{
					Status: http.StatusBadRequest,
					Code:   invalidJSONCode,
				}
			}
			return service.AdminTarget(ctx, actor, event, id)
		},
	)
}

func (c Client) localAssignPass(
	ctx context.Context,
	owner string,
	command passbooking.AdminAssignment,
) (passbooking.AdminAssignmentResult, error) {
	return directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (passbooking.AdminAssignmentResult, error) {
			return service.AdminAssign(ctx, actor, command)
		},
	)
}

func (c Client) localPassTakeoverTarget(
	ctx context.Context,
	owner, event string,
	id int64,
) (passbooking.TakeoverTarget, error) {
	return directRegistration(
		ctx,
		c,
		owner,
		func(service passbooking.Service, actor string) (passbooking.TakeoverTarget, error) {
			return service.TakeoverTarget(ctx, actor, event, id)
		},
	)
}
