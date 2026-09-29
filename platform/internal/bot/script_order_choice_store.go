package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (b *Bot) resolveModernChoice(
	ctx context.Context,
	owner, name string,
	args *modernOrderArguments,
	record *agenthost.ScriptToolRecord,
) error {
	if args.ChoiceRef == "" {
		return nil
	}
	draft, _, err := b.currentModernChoice(ctx, owner, args.ChoiceRef)
	if err != nil {
		return err
	}
	if args.Event != "" && args.Event != draft.Event {
		return errors.New("choice event mismatch")
	}
	if name == modernOrdersUpdate {
		if (args.Name != actionCreateOrder && args.Name != modernOrderEdit) || args.OrderID != draft.OrderID {
			return errors.New("choice target mismatch")
		}
		if record == nil {
			return errors.New("choice command binding missing")
		}
		record.ChoiceUse = args.ChoiceRef
		record.ChoiceCatalog = draft.Catalog
		record.ChoiceGeneration = draft.HistoryGeneration
	}
	choice := draft.Choice.Input()
	args.Event, args.Choice = draft.Event, &choice
	return nil
}

func (b *Bot) currentModernChoice(
	ctx context.Context,
	owner, ref string,
) (agenthost.ModernChoiceRecord, orders.Event, error) {
	draft, err := b.scriptHost().Store.LoadModernChoice(ctx, owner, ref)
	if err != nil {
		return draft, orders.Event{}, err
	}
	event, err := b.API.OrderEvent(ctx, owner, draft.Event)
	if err != nil {
		return draft, event, err
	}
	if modernCatalogFingerprint(event) != draft.Catalog {
		return draft, event, appclient.ErrReadStale
	}
	if draft.OrderID != "" {
		order, getErr := b.API.Order(ctx, owner, draft.Event, draft.OrderID)
		if getErr != nil {
			return draft, event, getErr
		}
		fingerprint, hashErr := modernOrderFingerprint(order)
		if hashErr != nil {
			return draft, event, hashErr
		}
		if fingerprint != draft.Snapshot {
			return draft, event, appclient.ErrReadStale
		}
	}
	return draft, event, b.validateModernChoiceGeneration(ctx, owner, draft)
}

func (b *Bot) validateModernChoiceGeneration(
	ctx context.Context,
	owner string,
	draft agenthost.ModernChoiceRecord,
) error {
	if draft.HistoryGeneration == nil {
		return appclient.ErrReadStale
	}
	return b.API.CheckHistoryGeneration(ctx, owner, *draft.HistoryGeneration)
}
