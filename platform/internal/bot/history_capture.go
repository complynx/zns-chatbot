package bot

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/interaction"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const historySystemKind = "system"

const historyReply = "reply"

const historyOrdersReply = "orders_reply"
const historyProfileReply = "profile_reply"

func (b *Bot) archiveUserRequest(ctx context.Context, in incoming, id int64, cached interaction.SavedPlan) error {
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
	return b.Host.ArchiveOriginal(ctx, in.owner, "tg-user-"+strconv.FormatInt(id, 10), "user", text)
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
	return b.Host.ArchiveOriginal(ctx, in.owner, "tg-user-"+strconv.FormatInt(u.ID, 10), kind, text)
}

func (b *Bot) archiveReply(
	ctx context.Context,
	owner string,
	id int64,
	kind string,
	content any,
	origin interaction.ReplyOrigin,
) error {
	if kind == "order_notification" {
		notice, ok := content.(map[string]string)
		if !ok || notice[originField] != historySystemKind {
			return nil
		}
		return b.Host.ArchiveOutcome(ctx, owner, "notification-"+strconv.FormatInt(id, 10), notice[textField])
	}
	if origin == interaction.DerivedReply {
		if err := b.planAuthorization().ValidateReply(ctx, owner, id); err != nil {
			return err
		}
	}
	var text string
	switch kind {
	case historyReply, historyOrdersReply, knowledgeReply, registrationReply:
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
	if origin == interaction.TrustedReply {
		return b.Host.ArchiveOutcome(ctx, owner, "tg-assistant-"+strconv.FormatInt(id, 10), text)
	}
	if origin != interaction.DerivedReply {
		return errors.New("invalid reply origin")
	}
	return b.archiveAssistantReply(ctx, owner, id, text)
}

func (b *Bot) archiveAssistantReply(ctx context.Context, owner string, id int64, text string) error {
	plan, err := (interaction.Store{DB: b.DB}).Load(ctx, owner, id)
	if err != nil {
		return err
	}
	if err = b.planAuthorization().ValidateDerivedReply(ctx, owner, id, plan); err != nil {
		return err
	}
	if plan.PassAuthority == nil {
		return errors.New("missing derived reply authority")
	}
	media := plan.MediaID != "" || len(plan.AVIDs) > 0
	err = b.Host.ArchiveDerived(
		ctx,
		owner,
		"tg-assistant-"+strconv.FormatInt(id, 10),
		text,
		id,
		media,
		plan.HistoryGeneration,
		plan.PassAuthority.ReadAuthorities,
	)
	if core.IsDatabaseFailure(err) {
		return err
	}
	var problem *core.ProblemError
	if errors.As(err, &problem) && problem.Code == historyStale {
		if passErr := b.planAuthorization().ValidateAuthority(ctx, owner, id, plan); passErr != nil {
			return passErr
		}
		return b.planAuthorization().
			ValidateHistoryPlan(ctx, owner, id, interaction.SavedPlan{TerminalReason: interaction.HistoryDeleted})
	}
	if err == nil {
		return b.planAuthorization().ValidateDerivedReply(ctx, owner, id, plan)
	}
	return err
}
