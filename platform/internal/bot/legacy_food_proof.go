package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func (b *Bot) showFoodProof(ctx context.Context, in incoming, command legacyfood.Command) error {
	family := botFamilyFoodProofMeals
	if command.Kind == legacyfood.Activity {
		family = botFamilyFoodProofActivity
	} else if command.Kind != legacyfood.Meals {
		return botdelivery.ErrBinding
	}
	_, err := b.queueBotDocument(
		ctx,
		in.owner,
		in.chat,
		botdelivery.Reference{
			Family:       family,
			Event:        command.EventID,
			Object:       command.OrderID,
			Version:      command.Version,
			Attempt:      command.Generation,
			Continuation: botdelivery.Continuation{Kind: botDocumentKind},
		},
	)
	return err
}
