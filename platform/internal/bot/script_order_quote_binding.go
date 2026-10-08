package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// prepareModernOrderChoice binds referenced quotes without hydrating the choice
// twice. Mutations retain full preparation and execution retains live validation.
func (b *Bot) prepareModernOrderChoice(
	ctx context.Context,
	owner, name string,
	args *modernOrderArguments,
	record *agenthost.ScriptToolRecord,
) error {
	if name != modernOrdersQuote || args.ChoiceRef == "" {
		return b.resolveModernChoice(ctx, owner, name, args, record)
	}
	metadata, err := b.scriptHost().Store.LoadModernChoiceMetadata(ctx, owner, args.ChoiceRef)
	if err != nil {
		return err
	}
	if args.Event != "" && args.Event != metadata.Event {
		return errors.New("choice event mismatch")
	}
	snapshot, err := b.API.OrderChoiceSnapshot(ctx, owner, metadata.Event, metadata.OrderID)
	if err != nil {
		return err
	}
	if snapshot.Catalog != metadata.Catalog || snapshot.Order != metadata.Snapshot {
		return appclient.ErrReadStale
	}
	if err = b.API.CheckHistoryGeneration(ctx, owner, metadata.HistoryGeneration); err != nil {
		return err
	}
	args.Event = metadata.Event
	return nil
}

// A successful draft is already canonical. Revalidate its current sources and
// booking permission before exposing each page, rather than quoting its full
// private body again. The receipt is loaded anew, including consumed/redacted state.
func (b *Bot) currentModernQuote(ctx context.Context, owner, event, ref string) (orders.Choice, error) {
	draft, err := b.scriptHost().Store.LoadModernChoice(ctx, owner, ref)
	if err != nil {
		return orders.Choice{}, err
	}
	if draft.Event != event {
		return orders.Choice{}, errors.New("choice event mismatch")
	}
	snapshot, err := b.API.OrderChoiceSnapshot(ctx, owner, event, draft.OrderID)
	if err != nil {
		return orders.Choice{}, err
	}
	if snapshot.Catalog != draft.Catalog || snapshot.Order != draft.Snapshot {
		return orders.Choice{}, appclient.ErrReadStale
	}
	if err = b.validateModernChoiceGeneration(ctx, owner, draft); err != nil {
		return orders.Choice{}, err
	}
	if !snapshot.CanBook {
		return orders.Choice{}, &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return draft.Choice, nil
}
