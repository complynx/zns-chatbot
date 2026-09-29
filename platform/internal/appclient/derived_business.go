package appclient

import (
	"context"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (c Host) ExecuteDerivedFood(
	ctx context.Context,
	owner string,
	command legacyfood.Command,
	source readsource.Derivation,
) (legacyfood.Order, error) {
	var result legacyfood.Order
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (legacyfood.Order, error) {
				return s.ExecuteFood(ctx, actor, command, source.Clone())
			}),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/food-actions", command, source, &result)
	return result, err
}

func (c Host) ExecuteDerivedMassage(
	ctx context.Context,
	owner string,
	command massage.Command,
	source readsource.Derivation,
) (massage.Reservation, error) {
	var result massage.Reservation
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (massage.Reservation, error) {
				return s.ExecuteMassage(ctx, actor, command, source.Clone())
			}),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/massage-actions", command, source, &result)
	return result, err
}

func (c Host) SetDerivedMassagePreferences(
	ctx context.Context,
	owner, event string,
	command massage.Preferences,
	source readsource.Derivation,
) (massage.Preferences, error) {
	var result massage.Preferences
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (massage.Preferences, error) {
				return s.SetMassagePreferences(ctx, actor, event, command, source.Clone())
			}),
		)
	}
	err := c.derivedRequest(
		ctx,
		owner,
		"/internal/derived/massage-preferences?event="+url.QueryEscape(event),
		command,
		source,
		&result,
	)
	return result, err
}
