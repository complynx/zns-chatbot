package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Food targets appear only after receipt intent was explicitly established.
// The pending payment prompt does not consume uploads or unrelated text.
func (b *Bot) foodMediaMarkup(
	ctx context.Context,
	owner, language, id string,
	markup *telegram.Markup,
	hint *agent.MediaHint,
) error {
	view, err := b.API.foodView(ctx, owner, "", "", false)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range []struct {
		payment legacyfood.Payment
		label   i18n.ID
		total   legacyfood.Amount
	}{{view.Order.MealPayment, i18n.FoodMeals, view.Order.MealTotal}, {view.Order.ActivityPayment, i18n.FoodActivities, view.Order.ActivityTotal}} {
		if entry.payment.Locked() || entry.total <= 0 {
			continue
		}
		label, translateErr := foodReceiptChoiceLabel(
			language,
			entry.label,
			entry.payment.Kind,
			view.Order.PaymentAdmin == "",
		)
		if translateErr != nil {
			return translateErr
		}
		command := legacyfood.Command{
			EventID:    view.Event.ID,
			OrderID:    view.Order.ID,
			Version:    view.Order.Version,
			Name:       foodSubmitProof,
			Kind:       entry.payment.Kind,
			Generation: entry.payment.Generation,
			Key:        id,
		}
		if view.Order.PaymentAdmin == "" {
			command.Name = foodBeginPayment
		}
		button, buttonErr := b.foodButton(
			ctx,
			owner,
			label+" · "+view.Order.ID,
			foodButtonCommand{Command: command, MediaID: id},
		)
		if buttonErr != nil {
			return buttonErr
		}
		markup.Rows = append(markup.Rows, []telegram.Button{button})
		if command.Name == foodBeginPayment {
			hint.Choices = append(hint.Choices, agent.MediaChoice{Action: "food_prepare_" + command.Kind,
				OrderID: command.OrderID, Version: command.Version, Label: button.Text})
			continue
		}
		hint.Choices = append(
			hint.Choices,
			agent.MediaChoice{
				FoodTarget: &agent.FoodReceiptTarget{EventID: command.EventID, OrderID: command.OrderID,
					Version: command.Version, Kind: command.Kind, Generation: command.Generation},
				Action:  "food_" + entry.payment.Kind,
				OrderID: view.Order.ID,
				Version: view.Order.Version,
				Label:   button.Text,
			},
		)
	}
	return nil
}

func foodReceiptChoiceLabel(language string, label i18n.ID, kind string, prepare bool) (string, error) {
	if prepare {
		label = i18n.FoodPayMeals
		if kind == legacyfood.Activity {
			label = i18n.FoodPayActivities
		}
	}
	return i18n.Translate(language, label, nil)
}

func (b *Bot) chooseFoodReceipt(ctx context.Context, in incoming, choice foodButtonCommand) error {
	return b.selectFoodReceipt(ctx, in, choice, originManual)
}

func (b *Bot) selectFoodReceipt(ctx context.Context, in incoming, choice foodButtonCommand, origin string) error {
	var item mediaIntake
	err := b.DB.QueryRow(ctx, `UPDATE bot.media_intake SET food_command=$3,last_action='select_food',last_origin=$4 WHERE owner=$1 AND id=$2 AND status='choose' AND expires_at>now() AND command IS NULL AND registration_command IS NULL AND food_command IS NULL RETURNING id,attachment_id,food_command`, in.owner, choice.MediaID, choice.Command, origin).
		Scan(&item.ID, &item.AttachmentID, &item.FoodCommand)
	if errors.Is(err, pgx.ErrNoRows) {
		var expired bool
		item, expired, err = b.loadMediaUploadState(ctx, in.owner, choice.MediaID)
		if err == nil {
			if handled, resumeErr := b.resumeMediaIntake(ctx, in, item, expired); handled {
				return resumeErr
			}
			return b.foodNotice(ctx, in, i18n.FoodUnavailable)
		}
	}
	if err != nil {
		return b.foodFailure(ctx, in, err)
	}
	return b.commitFoodReceipt(ctx, in, item)
}

func (b *Bot) commitFoodReceipt(ctx context.Context, in incoming, item mediaIntake) error {
	command := *item.FoodCommand
	if command.ProofID == "" {
		proof, err := b.API.PromoteMedia(ctx, in.owner, item.AttachmentID)
		if err != nil {
			return b.mediaExecutionError(ctx, in, item, err)
		}
		command.ProofID = proof.ID
		if _, err = b.DB.Exec(
			ctx,
			`UPDATE bot.media_intake SET food_command=$3 WHERE owner=$1 AND id=$2`,
			in.owner,
			item.ID,
			command,
		); err != nil {
			return err
		}
	}
	item.FoodCommand = &command
	order, err := b.API.ExecuteFood(ctx, in.owner, command)
	if err != nil {
		return b.mediaExecutionError(ctx, in, item, err)
	}
	if _, err = b.DB.Exec(
		ctx,
		`UPDATE bot.media_intake SET status='done',notice=$3,model_text='' WHERE owner=$1 AND id=$2`,
		in.owner,
		item.ID,
		string(i18n.MediaSaved),
	); err != nil {
		return err
	}
	if err = b.renderFood(ctx, in, order.EventID, order.ID, false); err != nil {
		return err
	}
	return b.RenderMedia(ctx, in.owner, in.chat, item.ID)
}
