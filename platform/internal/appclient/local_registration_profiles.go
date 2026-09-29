package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func directPassProfile[T any](
	ctx context.Context,
	c Client,
	owner string,
	operation func(passes.Service, string) (T, error),
) (T, error) {
	var zero T
	actor, err := c.registrationActor(ctx, owner)
	if err != nil {
		return zero, err
	}
	value, err := operation(c.LocalRegistration.Profile, actor)
	if err != nil {
		return zero, orderApplicationError(err)
	}
	return value, nil
}

func (c Client) localPassProfile(ctx context.Context, owner string) (passes.Profile, error) {
	return directPassProfile(
		ctx,
		c,
		owner,
		func(service passes.Service, actor string) (passes.Profile, error) { return service.Get(ctx, actor) },
	)
}

func (c Client) localPassProfileHistory(ctx context.Context, owner string) ([]passes.Change, error) {
	return directPassProfile(ctx, c, owner, func(service passes.Service, actor string) ([]passes.Change, error) {
		return service.History(ctx, actor)
	})
}

func (c Client) localExecutePassProfile(
	ctx context.Context,
	owner string,
	command passes.Command,
) (passes.Profile, error) {
	return directPassProfile(ctx, c, owner, func(service passes.Service, actor string) (passes.Profile, error) {
		return service.Execute(ctx, actor, command)
	})
}

func (c Client) localPassProfileHistoryPage(
	ctx context.Context,
	owner string,
	before int64,
) (passes.HistoryPage, error) {
	return directPassProfile(ctx, c, owner, func(service passes.Service, actor string) (passes.HistoryPage, error) {
		return service.HistoryPage(ctx, actor, before)
	})
}
