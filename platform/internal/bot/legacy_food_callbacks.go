package bot

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const foodPrefix = "food:"
const legacyFoodPrefix = "food|"
const foodShowProof = "food_show_proof"
const foodBeginPayment = "begin_payment"
const foodSubmitProof = "submit_proof"
const foodExportFilenameField = "filename"
const foodExportMessageIDField = "message_id"

type foodButtonCommand struct {
	Command legacyfood.Command `json:"command"`
	MediaID string             `json:"media_id,omitempty"`
}

func (b *Bot) foodButton(ctx context.Context, owner, label string, command foodButtonCommand) (telegram.Button, error) {
	if _, err := b.DB.Exec(
		ctx,
		`DELETE FROM bot.food_buttons WHERE ctid IN (SELECT ctid FROM bot.food_buttons WHERE expires_at<=now() AND consumed_at IS NULL ORDER BY expires_at LIMIT 100)`,
	); err != nil {
		return telegram.Button{}, err
	}
	token := rand.Text()
	_, err := b.DB.Exec(
		ctx,
		`INSERT INTO bot.food_buttons(owner,token,command) VALUES($1,$2,$3)`,
		owner,
		token,
		command,
	)
	return telegram.Button{Text: label, Data: foodPrefix + token}, err
}

// Old callback data has no attempt token. Pin its first authorized meaning so a
// later click can never rebind it to a replacement payment or another event.
func (b *Bot) bindFoodLegacy(ctx context.Context, in incoming) (legacyfood.Callback, error) {
	digest := sha256.Sum256([]byte(in.text))
	hash := hex.EncodeToString(digest[:])
	var saved legacyfood.Callback
	err := b.DB.QueryRow(ctx, `SELECT binding FROM bot.food_legacy_callbacks WHERE owner=$1 AND callback_hash=$2`, in.owner, hash).
		Scan(&saved)
	if err == nil {
		return saved, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return saved, err
	}
	saved, err = b.API.ResolveFood(ctx, in.owner, "", in.text)
	if err != nil {
		return saved, err
	}
	_, err = b.DB.Exec(
		ctx,
		`INSERT INTO bot.food_legacy_callbacks(owner,callback_hash,binding) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		in.owner,
		hash,
		saved,
	)
	if err == nil {
		err = b.DB.QueryRow(ctx, `SELECT binding FROM bot.food_legacy_callbacks WHERE owner=$1 AND callback_hash=$2`, in.owner, hash).
			Scan(&saved)
	}
	return saved, err
}

func (b *Bot) handleFood(ctx context.Context, in incoming, update telegram.Update) error {
	if update.Callback != nil {
		defer b.acknowledge(ctx, update.Callback.ID)
	}
	if in.text == "/exportfoodorders" {
		return b.exportFood(ctx, in, update.ID)
	}
	var choice foodButtonCommand
	var err error
	if strings.HasPrefix(in.text, legacyFoodPrefix) {
		var binding legacyfood.Callback
		binding, err = b.bindFoodLegacy(ctx, in)
		choice.Command = binding.Command
		if choice.Command.Name == "" {
			choice.Command.Name = binding.Action
		}
	} else {
		err = b.DB.QueryRow(ctx, `UPDATE bot.food_buttons SET consumed_at=COALESCE(consumed_at,now()) WHERE owner=$1 AND token=$2 AND (expires_at>now() OR consumed_at IS NOT NULL) RETURNING command`, in.owner, strings.TrimPrefix(in.text, foodPrefix)).
			Scan(&choice)
	}
	if err != nil {
		return b.foodFailure(ctx, in, err)
	}
	if choice.MediaID != "" {
		if choice.Command.Name == foodBeginPayment {
			choice.Command.Key = fmt.Sprintf("tg-food-%d", update.ID)
			if err = b.performFood(ctx, in, update.ID, choice.Command); err != nil {
				return err
			}
			return b.RenderMedia(ctx, in.owner, in.chat, choice.MediaID)
		}
		return b.chooseFoodReceipt(ctx, in, choice)
	}
	if choice.Command.Name == "exit" || choice.Command.Name == "activities_exit" {
		return b.closeFood(ctx, in, update, choice.Command.EventID)
	}
	choice.Command.Key = fmt.Sprintf("tg-food-%d", update.ID)
	return b.performFood(ctx, in, update.ID, choice.Command)
}

func (b *Bot) closeFood(ctx context.Context, in incoming, update telegram.Update, event string) error {
	if _, err := b.API.FoodView(ctx, in.owner, event, ""); err != nil {
		return b.foodFailure(ctx, in, err)
	}
	text, err := b.orderMessage(ctx, in.owner, i18n.FoodClosed, nil)
	if err != nil {
		return err
	}
	_, err = b.editOrSend(
		ctx,
		telegram.Send{
			ChatID:    in.chat,
			MessageID: update.Callback.Message.ID,
			Text:      text,
			Markup:    telegram.Markup{Rows: [][]telegram.Button{}},
		},
	)
	return err
}

func (b *Bot) performFood(ctx context.Context, in incoming, update int64, command legacyfood.Command) error {
	switch command.Name {
	case foodShowProof:
		return b.showFoodProof(ctx, in, command)
	case "submit_activities":
		return b.renderFood(ctx, in, command.EventID, command.OrderID, false)
	}
	order, err := b.API.ExecuteFood(ctx, in.owner, command)
	if err != nil {
		if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Code == "stale_version" {
			if err = b.foodNotice(ctx, in, i18n.FoodUnavailable); err != nil {
				return err
			}
			return b.renderFood(ctx, in, command.EventID, command.OrderID, false)
		}
		return b.foodFailure(ctx, in, err)
	}
	return b.finishFoodCommand(ctx, in, update, command, order)
}

func (b *Bot) finishFoodCommand(
	ctx context.Context,
	in incoming,
	update int64,
	command legacyfood.Command,
	order legacyfood.Order,
) error {
	return b.finishOrderedFoodCommand(ctx, in, update, command, order, 0)
}

func (b *Bot) finishOrderedFoodCommand(
	ctx context.Context,
	in incoming,
	update int64,
	command legacyfood.Command,
	order legacyfood.Order,
	sequence int,
) error {
	if command.Name == foodBeginPayment {
		payment := order.MealPayment
		if command.Kind == legacyfood.Activity {
			payment = order.ActivityPayment
		}
		command.Name, command.Version, command.Generation = foodSubmitProof, order.Version, payment.Generation
		command.CatalogRevision = ""
		err := b.storeFoodPending(ctx, in.owner, update, command, sequence)
		if err != nil {
			return err
		}
		if err = b.foodNotice(ctx, in, i18n.FoodReceipt); err != nil {
			return err
		}
	}
	return b.renderFood(ctx, in, order.EventID, order.ID, order.Owner != in.owner)
}

func (b *Bot) foodFailure(ctx context.Context, in incoming, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return b.foodNotice(ctx, in, i18n.FoodUnavailable)
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return b.foodNotice(ctx, in, i18n.FoodUnavailable)
	}
	return err
}

func (b *Bot) foodNotice(ctx context.Context, in incoming, id i18n.ID) error {
	text, err := b.orderMessage(ctx, in.owner, id, nil)
	if err != nil {
		return err
	}
	return b.deliverOrderCard(ctx, in.owner, "food:notice", telegram.Send{ChatID: in.chat, Text: text})
}

func (b *Bot) exportFood(ctx context.Context, in incoming, update int64) error {
	view, err := b.API.foodView(ctx, in.owner, "", "", false)
	if err != nil {
		return b.foodFailure(ctx, in, err)
	}
	if err = b.exportFoodEvent(ctx, in, update, view.Event.ID); err != nil {
		return b.foodFailure(ctx, in, err)
	}
	return b.foodNotice(ctx, in, i18n.FoodExported)
}

func (b *Bot) exportFoodEvent(ctx context.Context, in incoming, update int64, event string) error {
	exported, err := b.API.ExportFood(ctx, in.owner, event)
	if err != nil {
		return err
	}
	for _, file := range []struct {
		name, receipt string
		body          []byte
	}{
		{"food_orders_" + event + ".csv", "food_orders_export", exported.Orders},
		{"meal_summary_" + event + ".csv", "food_summary_export", exported.Summary},
	} {
		if err = b.deliverFoodExport(ctx, in, update, event, file.receipt, file.name, file.body); err != nil {
			return err
		}
	}
	return nil
}

// A durable receipt skips completed files. Telegram and this write are not atomic;
// a lost response or crash after sending can still leave an unknown delivery.
func (b *Bot) deliverFoodExport(
	ctx context.Context,
	in incoming,
	update int64,
	event, receipt, filename string,
	body []byte,
) error {
	var sent bool
	err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3)`,
		in.owner, update, receipt).
		Scan(&sent)
	if err != nil || sent {
		return err
	}
	if err = b.foodExportAllowed(ctx, in.owner, event); err != nil {
		return err
	}
	message, err := b.TG.SendDocument(ctx, in.chat, filename, body)
	if err != nil {
		return err
	}
	return b.record(ctx, in.owner, update, receipt, map[string]any{
		originField: in.origin, foodExportFilenameField: filename, foodExportMessageIDField: message.ID,
	})
}
