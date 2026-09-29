package bot

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type mediaView struct {
	ID    string
	Owner string
	Chat  int64
}

// Page through open, visible cards; completed history needs no periodic refresh.
func (b *Bot) reconcileMediaViews(ctx context.Context) error {
	if err := b.purgeExpiredAV(ctx); err != nil {
		return err
	}
	const pageSize = 100
	after := ""
	for {
		rows, err := b.DB.Query(ctx, `SELECT m.id,m.owner,c.chat_id
FROM bot.media_intake m JOIN bot.order_cards c ON c.owner=m.owner AND c.card_key='media:'||m.id
WHERE m.status<>'done' AND m.id>$1 ORDER BY m.id LIMIT $2`, after, pageSize)
		if err != nil {
			return err
		}
		views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[mediaView])
		if err != nil {
			return err
		}
		for _, view := range views {
			if err = b.reconcileMediaView(ctx, view); err != nil {
				b.logger().WarnContext(ctx, "media view reconciliation pending")
			}
			after = view.ID
		}
		if len(views) < pageSize {
			return nil
		}
	}
}

func (b *Bot) reconcileMediaView(ctx context.Context, view mediaView) error {
	ctx, authErr := b.API.NotificationContext(ctx, view.Owner, view.Chat)
	if authErr != nil {
		return authErr
	}
	item, expired, err := b.loadMediaUploadState(ctx, view.Owner, view.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return b.retireMediaView(ctx, view)
	}
	if err != nil {
		return err
	}
	in := incoming{owner: view.Owner, chat: view.Chat}
	if handled, resumeErr := b.resumeMediaIntake(ctx, in, item, expired); handled {
		return resumeErr
	}
	var metadata media.Attachment
	err = b.API.Call(ctx, view.Owner, http.MethodGet, "/v1/media/"+url.PathEscape(item.AttachmentID), nil, &metadata)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok &&
		(problem.Status == http.StatusForbidden || problem.Status == http.StatusNotFound) {
		return b.retireMediaView(ctx, view)
	}
	if err != nil {
		return err
	}
	err = b.RenderMedia(ctx, view.Owner, view.Chat, view.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return b.retireMediaView(ctx, view)
	}
	return err
}

func (b *Bot) retireMediaView(ctx context.Context, view mediaView) error {
	item, _, err := b.loadMediaUploadState(ctx, view.Owner, view.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if item.Status == mediaDone {
		return b.RenderMedia(ctx, view.Owner, view.Chat, view.ID)
	}
	if item.Command != nil && item.Command.ProofFile != "" {
		return b.commitMediaReceipt(ctx, incoming{owner: view.Owner, chat: view.Chat}, item)
	}
	if item.RegistrationCommand != nil && item.RegistrationCommand.ProofID != "" {
		return b.commitRegistrationReceipt(ctx, incoming{owner: view.Owner, chat: view.Chat}, item)
	}
	if item.FoodCommand != nil && item.FoodCommand.ProofID != "" {
		return b.commitFoodReceipt(ctx, incoming{owner: view.Owner, chat: view.Chat}, item)
	}
	pref, err := b.API.Preferences(ctx, view.Owner)
	if err != nil {
		return err
	}
	title, err := i18n.Translate(pref.Language, i18n.MediaTitle, map[string]string{"id": view.ID})
	if err != nil {
		return err
	}
	notice, err := i18n.Translate(pref.Language, i18n.MediaUnavailable, nil)
	if err != nil {
		return err
	}
	err = b.deliverOrderCard(ctx, view.Owner, mediaPrefix+view.ID, telegram.Send{
		ChatID: view.Chat, Text: title + "\n" + notice, Markup: telegram.Markup{Rows: [][]telegram.Button{}},
	})
	if err != nil {
		return err
	}
	_, err = b.DB.Exec(ctx, `UPDATE bot.media_intake SET status='done',notice=$3,model_text='',rendered=NULL
WHERE owner=$1 AND id=$2 AND status<>'done'`, view.Owner, view.ID, string(i18n.MediaUnavailable))
	return err
}
