package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/stickerassets"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type AssetDescriber interface {
	Describe(context.Context, telegram.Message) (agent.AssetContext, error)
}

type admittedAssets struct {
	describer AssetDescriber
	message   telegram.Message
}

func (a admittedAssets) Describe(ctx context.Context) (agent.AssetContext, error) {
	return a.describer.Describe(ctx, a.message)
}

func (b *Bot) assetSource(in incoming) agenthost.AssetSource {
	if in.assetMessage == nil {
		return nil
	}
	describer := b.Stickers
	if describer == nil {
		describer = stickerassets.Service{}
	}
	return admittedAssets{describer: describer, message: *in.assetMessage}
}
