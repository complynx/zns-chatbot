package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
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
