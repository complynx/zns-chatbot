package bot

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

// botdelivery.StoredResult lives with private interaction results, not the transport
// index. History generation changes remove its body transactionally.

// queueBotResult stores an already computed bounded private result once. Retry
// never reruns a model, mutates the body or invents a provider message ID.
func (b *Bot) queueBotResult(
	ctx context.Context,
	owner string,
	chat, update int64,
	effect string,
	ref botdelivery.Reference,
	result botdelivery.StoredResult,
	target int64,
) error {
	return b.Host.EnqueueBotResult(
		ctx,
		botdelivery.ResultRequest{
			Owner:     owner,
			Chat:      chat,
			Update:    update,
			Effect:    effect,
			Reference: ref,
			Result:    result,
			Target:    target,
		},
	)
}

func (b *Bot) renderBotResult(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	var saved botdelivery.StoredResult
	err := b.DB.QueryRow(ctx, "SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3", i.Owner, i.Reference.Update, i.Reference.ResultKind).
		Scan(&saved)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	if i.Reference.Generation == nil || saved.Generation != *i.Reference.Generation {
		return botRenderedDelivery{}, botdelivery.ErrStale
	}
	if err = b.API.CheckHistoryGeneration(ctx, i.Owner, saved.Generation); err != nil {
		return botRenderedDelivery{}, err
	}
	if err = b.authorizeBotResult(ctx, i); err != nil {
		return botRenderedDelivery{}, err
	}
	prefs, err := b.API.Preferences(ctx, i.Owner)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	if saved.Notice != "" {
		saved.Payload.Text, err = i18n.Translate(prefs.Language, saved.Notice, saved.Values)
		if err != nil {
			return botRenderedDelivery{}, err
		}
	}
	return botRenderedDelivery{Payload: saved.Payload, Receipt: i.Reference.Continuation}, nil
}

func (b *Bot) authorizeBotResult(ctx context.Context, i botdelivery.Intent) error {
	switch i.Reference.Family {
	case botFamilyStatic, "legacy_order":
		return b.checkOrderDeliverySource(ctx, i.Owner, i.Reference.Source)
	case botFamilyFoodClosed:
		_, err := b.API.FoodView(ctx, i.Owner, i.Reference.Event, "")
		return err
	case botFamilyAdminView, botFamilyAdminPage, botFamilyAdminPrompt, botFamilyAdminExpiry:
		return b.authorizeBotAdminResult(ctx, i)
	case "admin_utility":
		var authorized struct {
			OK bool `json:"ok"`
		}
		if err := b.API.Call(
			ctx,
			i.Owner,
			http.MethodGet,
			"/v1/admin-utilities/authorize",
			nil,
			&authorized,
		); err != nil {
			return err
		}
		if !authorized.OK {
			return botdelivery.ErrStale
		}
		return nil
	case botFamilyCredits:
		if i.Reference.Object == "" || i.Reference.Object == i.Owner {
			return nil
		}
		var permissions map[string]bool
		if err := b.API.Call(ctx, i.Owner, http.MethodGet, "/v1/credits/permissions", nil, &permissions); err != nil {
			return err
		}
		if !permissions["admin"] {
			return botdelivery.ErrStale
		}
		return nil
	case botFamilyModelSettings:
		var permissions map[string]bool
		if err := b.API.Call(
			ctx,
			i.Owner,
			http.MethodGet,
			"/v1/model-settings/permissions",
			nil,
			&permissions,
		); err != nil {
			return err
		}
		if !permissions[i.Reference.Object] {
			return botdelivery.ErrStale
		}
		return nil
	case "pass_tier":
		_, err := b.API.PassTierStatus(ctx, i.Owner, i.Reference.Event)
		return err
	default:
		return botdelivery.ErrBinding
	}
}

// bindBotResultGeneration pins the result to its observed source or current history.

// storeBotResult keeps the first private body and verifies its immutable source.

func (b *Bot) authorizeBotAdminResult(ctx context.Context, i botdelivery.Intent) error {
	switch i.Reference.Family {
	case botFamilyAdminView, botFamilyAdminPage, botFamilyAdminPrompt:
		if err := b.authorizeAdminMessage(ctx, i.Owner); err != nil {
			return err
		}
		if i.Reference.Family == botFamilyAdminPage {
			return b.API.CheckAdminMessagePublication(ctx, i.Owner, i.Reference.Version)
		}
		if i.Reference.Family == botFamilyAdminPrompt {
			return b.authorizeBotAdminPrompt(ctx, i)
		}
		return nil
	case botFamilyAdminExpiry:
		expiry, found, err := b.Host.CurrentAdminInputExpiry(ctx, i.Reference.Version)
		if err != nil {
			return err
		}
		if !found || expiry.Actor != i.Owner || expiry.ChatID != i.Chat {
			return botdelivery.ErrStale
		}
		return nil
	default:
		return botdelivery.ErrBinding
	}
}

func (b *Bot) authorizeBotAdminPrompt(ctx context.Context, i botdelivery.Intent) error {
	var pending []adminmessage.Input
	if err := b.API.Call(
		ctx,
		i.Owner,
		http.MethodPost,
		"/v1/admin-messages/input/pending",
		map[string]int64{broadcastChatKey: i.Chat},
		&pending,
	); err != nil {
		return err
	}
	for _, input := range pending {
		if input.ID == i.Reference.Version && input.State == broadcastPending && input.PromptID == 0 {
			return nil
		}
	}
	return botdelivery.ErrStale
}
