package appclient

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// LocalBotDelivery preserves the authenticated application boundary in one process.
type LocalBotDelivery struct {
	Service    botdelivery.Service
	Authorizer applicationauth.Authorizer
}

func (c Host) EnqueueBotDelivery(ctx context.Context, in botdelivery.EnqueueRequest) (botdelivery.Observation, error) {
	return botDeliveryCall(
		ctx,
		c,
		in.Owner,
		"enqueue",
		in,
		func(s botdelivery.Service) (botdelivery.Observation, error) { return s.Enqueue(ctx, in) },
	)
}
func (c Host) EnqueueBotCard(ctx context.Context, in botdelivery.CardRequest) error {
	_, err := botDeliveryCall(
		ctx,
		c,
		in.Owner,
		"card",
		in,
		func(s botdelivery.Service) (struct{}, error) { return struct{}{}, s.EnqueueCard(ctx, in) },
	)
	return err
}
func (c Host) EnqueueBotResult(ctx context.Context, in botdelivery.ResultRequest) error {
	_, err := botDeliveryCall(
		ctx,
		c,
		in.Owner,
		"result",
		in,
		func(s botdelivery.Service) (struct{}, error) { return struct{}{}, s.EnqueueResult(ctx, in) },
	)
	return err
}
func (c Host) BeginBotDelivery(ctx context.Context, in botdelivery.BeginRequest) (botdelivery.BeginResult, error) {
	return botDeliveryCall(
		ctx,
		c,
		in.Observed.Owner,
		"begin",
		in,
		func(s botdelivery.Service) (botdelivery.BeginResult, error) { return s.Begin(ctx, in) },
	)
}

// Identity exchange happens before any application transaction. The ownerless
// unavailable-identity notice is separately constrained by the service contract.
func botDeliveryCall[T any](
	ctx context.Context,
	c Host,
	owner, action string,
	in any,
	local func(botdelivery.Service) (T, error),
) (T, error) {
	var zero T
	raw, err := json.Marshal(in)
	if err != nil {
		return zero, err
	}
	if len(raw) > botdelivery.MaxRequestBytes {
		return zero, botdelivery.ErrBinding
	}
	var token string
	if owner != "" {
		if c.UserToken == nil {
			return zero, identity.ErrZitadelIdentity
		}
		token, err = c.UserToken(ctx, owner)
		if err != nil {
			return zero, err
		}
	}
	if c.LocalBotDelivery != nil {
		if owner != "" {
			if _, err = authorizeOwnerStatus(
				ctx,
				c.LocalBotDelivery.Authorizer,
				token,
				owner,
				http.StatusUnauthorized,
			); err != nil {
				return zero, err
			}
		}
		result, callErr := local(c.LocalBotDelivery.Service)
		if callErr != nil {
			return zero, orderApplicationError(callErr)
		}
		return result, nil
	}
	var result T
	err = requestAuthorizedLimit(
		ctx,
		c.Base,
		c.HTTP,
		token,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/bot-delivery/"+action,
		raw,
		&result,
		int64(botdelivery.MaxRequestBytes),
	)
	if err != nil {
		return zero, err
	}
	return result, nil
}

func (c Host) ApplyBotDeliveryReceipt(ctx context.Context, in botdelivery.ReceiptRequest) error {
	_, err := botDeliveryCall(
		ctx,
		c,
		in.Observed.Owner,
		"receipt",
		in,
		func(s botdelivery.Service) (struct{}, error) { return struct{}{}, s.ApplyReceipt(ctx, in) },
	)
	return err
}

func (c Host) CheckBotDeliverySource(ctx context.Context, in botdelivery.SourceRequest) error {
	_, err := botDeliveryCall(
		ctx,
		c,
		in.Owner,
		"source",
		in,
		func(s botdelivery.Service) (struct{}, error) { return struct{}{}, s.CheckSource(ctx, in) },
	)
	return err
}
func (c Host) StoreBotPassMenu(ctx context.Context, in botdelivery.PassMenuRequest) error {
	_, err := botDeliveryCall(
		ctx,
		c,
		in.Owner,
		"pass-menu",
		in,
		func(s botdelivery.Service) (struct{}, error) { return struct{}{}, s.StorePassMenu(ctx, in) },
	)
	return err
}
