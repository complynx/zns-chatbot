package bot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const actionInstructions = orders.ActionPaymentInstructions
const paymentCardPrefix = "payment:"

func (b *Bot) showPaymentInstructions(ctx context.Context, in incoming, id string) (string, error) {
	return b.showPaymentInstructionsWithSource(ctx, in, id, nil)
}

func (b *Bot) showPaymentInstructionsWithSource(
	ctx context.Context,
	in incoming,
	id string,
	source *readsource.Derivation,
) (string, error) {
	return b.paymentInstructionsWithSource(ctx, in, id, source, true)
}

func (b *Bot) paymentInstructionsWithSource(
	ctx context.Context,
	in incoming,
	id string,
	source *readsource.Derivation,
	opening bool,
) (string, error) {
	if err := b.checkOrderDeliverySource(ctx, in.owner, source); err != nil {
		return "", err
	}
	event, err := b.OrderEventForOrder(ctx, in.owner, id)
	if err != nil {
		return b.proofFailure(ctx, in.owner, err)
	}
	info, err := b.API.PaymentInstructions(ctx, in.owner, event, id)
	if err != nil {
		if core.IsDatabaseFailure(err) {
			return "", core.ErrDatabase
		}
		if problem, ok := errors.AsType[*core.ProblemError](
			err,
		); ok &&
			problem.Status < http.StatusInternalServerError {
			return b.paymentInstructionsUnavailable(ctx, in, id, problem.Code)
		}
		return "", err
	}
	payload, err := paymentInstructionsPayload(in.chat, info)
	if err != nil {
		return "", err
	}
	if opening {
		if err = b.bindPaymentSource(ctx, in.owner, id, source); err != nil {
			return "", err
		}
	}
	check := func() error { return b.checkPaymentDelivery(ctx, in.owner, event, info, source) }
	if err = b.deliverOrderCardChecked(ctx, in.owner, paymentCardPrefix+id, payload, check); err != nil {
		return "", err
	}
	return i18n.Translate(info.Language, i18n.PaymentShown, nil)
}

func paymentInstructionsPayload(chat int64, info orders.PaymentInstructions) (telegram.Send, error) {
	text, err := paymentSummary(info)
	if err != nil {
		return telegram.Send{}, err
	}
	result := telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}}
	appendText := func(id i18n.ID, values map[string]string) {
		if err != nil {
			return
		}
		var value string
		value, err = i18n.Translate(info.Language, id, values)
		result.Text += "\n" + value
	}
	if info.CanPay {
		if info.Transfer != "" {
			result.Text += "\n\n" + info.Transfer
		} else {
			appendText(i18n.PaymentTransferUnavailable, nil)
		}
		appendText(i18n.PaymentCashChoice, nil)
	} else {
		appendText(i18n.PaymentNotAllowed, nil)
	}
	if info.State == stateCash {
		appendText(i18n.PaymentCashPending, nil)
	}
	for _, contact := range info.Contacts {
		if info.State == stateCash && contact.ID == info.PaymentAdmin {
			appendText(
				i18n.PaymentAdmin,
				map[string]string{
					"name":   fmt.Sprintf("%.120s", contact.Name),
					"region": fmt.Sprintf("%.80s", contact.Region),
				},
			)
		}
		if info.CanPay || (info.State == stateCash && contact.ID == info.PaymentAdmin) {
			label, labelError := i18n.Translate(
				info.Language,
				i18n.PaymentContact,
				map[string]string{
					"name":   fmt.Sprintf("%.40s", contact.Name),
					"region": fmt.Sprintf("%.40s", contact.Region),
				},
			)
			if labelError != nil {
				return telegram.Send{}, labelError
			}
			result.Markup.Rows = append(result.Markup.Rows, []telegram.Button{{
				Text: label,
				URL:  fmt.Sprintf("tg://user?id=%d", contact.TelegramID),
			}})
		}
	}
	return result, err
}

func paymentSummary(info orders.PaymentInstructions) (string, error) {
	state, err := i18n.Translate(info.Language, orderStateID(info.State), nil)
	if err != nil {
		return "", err
	}
	amount, err := info.TotalBYN.MarshalJSON()
	if err != nil {
		return "", err
	}
	byn, err := i18n.FormatNumber(info.Language, string(amount))
	if err != nil {
		return "", err
	}
	rub, err := i18n.FormatNumber(info.Language, info.TotalRUB)
	if err != nil {
		return "", err
	}
	return i18n.Translate(
		info.Language,
		i18n.PaymentSummary,
		map[string]string{
			mediaOrderChoice: info.OrderID,
			"state":          state,
			"version":        strconv.FormatInt(info.Version, 10),
			"byn":            byn,
			"rub":            rub,
		},
	)
}

func (b *Bot) paymentInstructionsUnavailable(ctx context.Context, in incoming, id, code string) (string, error) {
	preference, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return "", err
	}
	text, err := i18n.Translate(
		preference.Language,
		i18n.PaymentUnavailable,
		map[string]string{orderCodeParameter: code},
	)
	if err != nil {
		return "", err
	}
	var opened bool
	key := paymentCardPrefix + id
	if err = b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.order_cards WHERE owner=$1 AND card_key=$2)`, in.owner, key).
		Scan(&opened); err != nil {
		return "", core.DatabaseOperationError(err)
	}
	if opened {
		if retired, _ := ctx.Value(botRetiredCardKey{}).(bool); retired {
			ref, unavailable, bindingErr := b.paymentRetirementReference(ctx, in.owner, key, id)
			if bindingErr != nil || !unavailable {
				return text, bindingErr
			}
			ctx = withBotCard(ctx, ref)
		}
		err = b.deliverOrderCard(
			ctx,
			in.owner,
			key,
			telegram.Send{ChatID: in.chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}},
		)
	}
	return text, err
}

func (b *Bot) paymentRetirementReference(
	ctx context.Context,
	owner, key, id string,
) (botdelivery.Reference, bool, error) {
	var event, state string
	err := b.DB.QueryRow(ctx, `SELECT event_id,state FROM core.orders WHERE owner=$1 AND id=$2`, owner, id).
		Scan(&event, &state)
	if err != nil {
		return botdelivery.Reference{}, false, core.DatabaseOperationContextError(ctx, err)
	}
	if state != "deleted" {
		return botdelivery.Reference{}, false, nil
	}
	prior := &botdelivery.PaymentRetirement{}
	err = b.DB.QueryRow(ctx, `SELECT i.operation_key,i.effect_key,c.view_hash
 FROM bot.order_cards c JOIN bot.delivery_intents i
 ON i.owner=c.owner AND i.chat_id=c.chat_id AND i.message_id=c.message_id
 WHERE c.owner=$1 AND c.card_key=$2 AND c.visible AND c.message_id>0 AND i.bot_id=$3
 AND i.state='sent' AND i.reference->>'family'='payment' AND i.reference->>'card_key'=$2
 ORDER BY i.attempted_at DESC NULLS LAST,i.created_at DESC,i.operation_key DESC,i.effect_key DESC LIMIT 1`,
		owner, key, b.Delivery.BotID).Scan(&prior.Operation, &prior.Effect, &prior.ViewHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return botdelivery.Reference{}, false, botdelivery.ErrStale
	}
	if err != nil {
		return botdelivery.Reference{}, false, core.DatabaseOperationContextError(ctx, err)
	}
	return botdelivery.Reference{
		Kind: botdelivery.CardIntent, Family: registrationPayment, CardKey: key,
		Event: event, Object: id, Notice: i18n.PaymentUnavailable, PaymentRetirement: prior,
	}, true, nil
}

// A retirement reconstructs only a fixed notice for the existing payment card.
func (b *Bot) renderPaymentRetirement(ctx context.Context, i botdelivery.Intent) (botRenderedDelivery, error) {
	if !i.Reference.Continuation.Retired || i.Target <= 0 || i.Phase != botPhaseEdit {
		return botRenderedDelivery{}, botdelivery.ErrBinding
	}
	preference, err := b.API.Preferences(ctx, i.Owner)
	if err != nil {
		return botRenderedDelivery{}, err
	}
	text, err := i18n.Translate(preference.Language, i18n.PaymentUnavailable,
		map[string]string{orderCodeParameter: "payment_context_unavailable"})
	if err != nil {
		return botRenderedDelivery{}, err
	}
	payload := telegram.Send{
		ChatID: i.Chat, MessageID: i.Target, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}},
	}
	hash, err := botCardHash(payload)
	receipt := i.Reference.Continuation
	receipt.ViewHash = hash
	return botRenderedDelivery{Payload: payload, Receipt: receipt}, err
}

func (b *Bot) refreshPaymentInstructions(
	ctx context.Context,
	owner string,
	chat int64,
	order orders.Order,
	active map[string]bool,
) error {
	key := paymentCardPrefix + order.ID
	var opened bool
	if err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.order_cards WHERE owner=$1 AND card_key=$2)`, owner, key).
		Scan(&opened); err != nil {
		return core.DatabaseOperationError(err)
	}
	if !opened && order.State != stateCash {
		return nil
	}
	active[key] = true
	if !opened {
		_, err := b.showPaymentInstructions(ctx, incoming{owner: owner, chat: chat}, order.ID)
		return err
	}
	source, err := b.paymentSource(ctx, owner, order.ID)
	if err != nil {
		return err
	}
	_, err = b.paymentInstructionsWithSource(ctx, incoming{owner: owner, chat: chat}, order.ID, source, false)
	return err
}

func orderStateID(state string) i18n.ID {
	states := map[string]i18n.ID{
		"unpaid":  i18n.OrderStateUnpaid,
		"cash":    i18n.OrderStateCash,
		"proof":   i18n.OrderStateProof,
		statePaid: i18n.OrderStatePaid,
		"deleted": i18n.OrderStateDeleted,
	}
	return states[state]
}
