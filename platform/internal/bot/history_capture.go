package bot

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const historySystemKind = "system"

const historyOrdersReply = "orders_reply"
const historyProfileReply = "profile_reply"

func (b *Bot) archiveUserRequest(ctx context.Context, in incoming, id int64, cached cachedPlan) error {
	text := in.text
	if in.assetMessage != nil && in.assetMessage.Text != "" {
		text = in.assetMessage.Text
	}
	privateProfile, _, err := b.scriptProfileEffects(ctx, in.owner, id)
	if err != nil {
		return err
	}
	if privateProfile {
		text = "[private profile submission omitted]"
	}
	if cached.ProfileCommand != nil {
		text = "[private profile submission omitted]"
	}
	if in.mediaID != "" || len(cached.AVIDs) > 0 {
		text = "[media request; expiring content omitted]"
	}
	return b.API.ArchiveConversation(ctx, in.owner, "tg-user-"+strconv.FormatInt(id, 10), "user", text, 0, false)
}

func (b *Bot) archiveControl(ctx context.Context, in incoming, u telegram.Update) error {
	var text string
	kind := "manual"
	switch {
	case u.Callback != nil:
		prefix, _, _ := strings.Cut(in.text, ":")
		const maxControlPrefix = 100
		if len(prefix) > maxControlPrefix {
			prefix = "unknown"
		}
		text = "button: " + prefix
	case u.Message != nil && (u.Message.Document != nil || len(u.Message.Photo) > 0 || hasAV(*u.Message)):
		text = "[media upload; expiring content omitted]"
		kind = "user"
	case strings.HasPrefix(in.text, "/"):
		text = strings.Fields(in.text)[0]
	default:
		return nil
	}
	return b.API.ArchiveConversation(ctx, in.owner, "tg-user-"+strconv.FormatInt(u.ID, 10), kind, text, 0, false)
}

func (b *Bot) archiveReply(ctx context.Context, owner string, id int64, kind string, content any) error {
	if kind == "order_notification" {
		notice, ok := content.(map[string]string)
		if !ok || notice[originField] != historySystemKind {
			return nil
		}
		return b.API.ArchiveConversation(
			ctx,
			owner,
			"notification-"+strconv.FormatInt(id, 10),
			"system",
			notice[textField],
			0, false,
		)
	}
	var text string
	switch kind {
	case "reply", historyOrdersReply, knowledgeReply, registrationReply:
		text, _ = content.(string)
	case historyProfileReply, profileAnswer:
		text = "[private profile response omitted]"
	default:
		return nil
	}
	privateProfile, _, err := b.scriptProfileEffects(ctx, owner, id)
	if err != nil {
		return err
	}
	if privateProfile {
		text = "[private profile response omitted]"
	}
	if text == "" {
		return nil
	}
	return b.archiveAssistantReply(ctx, owner, id, text)
}

func (b *Bot) archiveAssistantReply(ctx context.Context, owner string, id int64, text string) error {
	var plan cachedPlan
	var generation *int64
	err := b.DB.QueryRow(ctx, `SELECT plan FROM bot.replies WHERE update_id=$1`, id).Scan(&plan)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		if plan.HistoryRedacted {
			return b.validateHistoryPlan(ctx, owner, id, plan)
		}
		generation = &plan.HistoryGeneration
	}
	media := plan.MediaID != "" || len(plan.AVIDs) > 0
	err = b.API.archiveConversation(
		ctx,
		owner,
		"tg-assistant-"+strconv.FormatInt(id, 10),
		"assistant",
		text,
		id, media, generation,
	)
	var problem *core.ProblemError
	if errors.As(err, &problem) && problem.Code == "history_stale" {
		return b.validateHistoryPlan(ctx, owner, id, cachedPlan{HistoryRedacted: true})
	}
	if err == nil && generation != nil {
		return b.validateHistoryPlan(ctx, owner, id, plan)
	}
	return err
}
