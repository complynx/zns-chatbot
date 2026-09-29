package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/stickerassets"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type AssetDescriber interface {
	Describe(context.Context, telegram.Message) (agent.AssetContext, error)
}

// Model planning has already loaded the owner's authorized API context. Artwork
// observations never become direct user text or authority for order selection.
func (b *Bot) addAssetContext(ctx context.Context, in incoming, input *agent.Input) error {
	if in.assetMessage == nil {
		return nil
	}
	describer := b.Stickers
	if describer == nil {
		describer = stickerassets.Service{}
	}
	assets, err := describer.Describe(ctx, *in.assetMessage)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		assets = agent.AssetContext{Items: []agent.AssetObservation{{Kind: "custom_emoji", Status: "unavailable"}}}
	}
	if len(assets.Items) > 0 || assets.Omitted > 0 {
		input.Assets = &assets
	}
	return nil
}
