package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const orderCallbackPrefix = "o:"
const stateCash = "cash"
const stateProof = "proof"
const stateUnpaid = "unpaid"
const actionField = "action"
const actionCreateOrder = "create"
const orderTotalKey = "total"

func (b *Bot) handleOrders(ctx context.Context, in incoming, update telegram.Update) error {
	notice, err := b.orderMessage(ctx, in.owner, i18n.OrdersHint, nil)
	if err != nil {
		return err
	}
	if err = b.rememberOrderLocale(ctx, in.owner, update.ID); err != nil {
		return err
	}
	if update.Callback != nil {
		notice, err = b.handleOrderCallback(ctx, in, update.ID)
		if err != nil {
			return err
		}
	}
	if err = b.record(ctx, in.owner, update.ID, "orders_reply", notice); err != nil {
		return err
	}
	if err = b.RenderOrders(ctx, in.owner, in.chat); err != nil {
		return err
	}
	if update.Callback != nil {
		b.acknowledge(ctx, update.Callback.ID)
	}
	return nil
}

func (b *Bot) handleOrderCallback(ctx context.Context, in incoming, update int64) (string, error) {
	if strings.HasPrefix(in.text, orderPagePrefix) {
		return b.handleOrderPage(ctx, in, update)
	}
	var command orders.Command
	err := b.DB.QueryRow(ctx, `SELECT command FROM bot.order_buttons WHERE owner=$1 AND token=$2`, in.owner,
		strings.TrimPrefix(in.text, orderCallbackPrefix)).Scan(&command)
	if errors.Is(err, pgx.ErrNoRows) {
		return b.orderMessage(ctx, in.owner, i18n.OrderUnavailable, nil)
	}
	if err != nil {
		return "", err
	}
	command.Key = fmt.Sprintf("tg-order-%d", update)
	if command.Name == actionExport {
		return b.exportOrders(ctx, in, update)
	}
	if command.Name == actionInstructions {
		return b.showPaymentInstructions(ctx, in, command.OrderID)
	}
	if command.Name == "upload_proof" {
		return b.selectProofOrder(ctx, in.owner, update, command)
	}
	if command.Name == "show_proof" {
		return b.showOrderProof(ctx, in, command)
	}
	return b.executeOrder(ctx, in.owner, update, command)
}

func (b *Bot) executeOrder(ctx context.Context, owner string, update int64, command orders.Command) (string, error) {
	if err := b.rememberOrderLocale(ctx, owner, update); err != nil {
		return "", err
	}
	updated, err := b.API.ExecuteOrder(ctx, owner, command)
	if err != nil {
		if problem, ok := errors.AsType[*core.ProblemError](
			err,
		); ok &&
			problem.Status < http.StatusInternalServerError {
			if recordErr := b.record(ctx, owner, update, "order_error", problem); recordErr != nil {
				return "", recordErr
			}
			return b.orderMessage(ctx, owner, i18n.OrderRejected, map[string]string{orderCodeParameter: problem.Code})
		}
		return "", err
	}
	if err = b.focusOrderPage(ctx, owner, updated, update); err != nil {
		return "", err
	}
	return b.orderMessage(ctx, owner, i18n.OrderUpdated, nil)
}

func (b *Bot) orderButton(ctx context.Context, owner, label string, command orders.Command) (telegram.Button, error) {
	if command.EventID == "" {
		command.EventID = b.currentOrderEvent()
	}
	command.Origin = originManual
	encoded, err := json.Marshal(command)
	if err != nil {
		return telegram.Button{}, err
	}
	digest := sha256.Sum256(encoded)
	token := hex.EncodeToString(digest[:16])
	_, err = b.DB.Exec(
		ctx,
		`INSERT INTO bot.order_buttons(owner,token,command) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		owner,
		token,
		encoded,
	)
	return telegram.Button{Text: label, Data: orderCallbackPrefix + token}, err
}

func (b *Bot) RenderOrders(ctx context.Context, owner string, chat int64) error {
	preference, err := b.API.Preferences(ctx, owner)
	if err != nil {
		return err
	}
	event, err := b.API.OrderEvent(ctx, owner, b.currentOrderEvent())
	if err != nil {
		return err
	}
	list, err := b.API.Orders(ctx, owner, event.ID)
	if err != nil {
		return err
	}
	admins, err := b.API.PaymentAdmins(ctx, owner, event.ID)
	if err != nil {
		return err
	}
	canExport := slices.ContainsFunc(admins, func(admin orders.PaymentAdmin) bool { return admin.ID == owner })
	if err = b.renderOrderMenu(ctx, owner, chat, canExport, preference.Language); err != nil {
		return err
	}
	active := map[string]bool{"menu": true, languageKey: true, "profile": true}
	available := map[string]bool{}
	list, err = b.pagedOrders(ctx, owner, chat, ownOrdersScope, preference.Language, list, active, available)
	if err != nil {
		return err
	}
	for _, order := range list {
		payload, renderError := b.orderPayload(ctx, owner, chat, order, event, admins, false, preference.Language)
		if renderError != nil {
			return renderError
		}
		active[order.ID] = true
		if err = b.deliverOrderCard(ctx, owner, order.ID, payload); err != nil {
			return err
		}
		if err = b.refreshPaymentInstructions(ctx, owner, chat, order, active); err != nil {
			return err
		}
	}
	if err = b.renderPaymentInbox(ctx, owner, chat, event, admins, active, available, preference.Language); err != nil {
		return err
	}
	return b.retireOrderCards(ctx, owner, chat, active, available, preference.Language)
}

func (b *Bot) renderOrderMenu(ctx context.Context, owner string, chat int64, canExport bool, language string) error {
	var raw []byte
	var native bool
	var replyLocale string
	var updateID int64
	err := b.DB.QueryRow(ctx, `SELECT r.content,r.native_markdown,r.update_id,COALESCE(l.content#>>'{}','') FROM bot.interactions r LEFT JOIN bot.interactions l ON l.owner=r.owner AND l.update_id=r.update_id AND l.kind='orders_reply_locale' WHERE r.owner=$1 AND r.kind='orders_reply' ORDER BY r.id DESC LIMIT 1`, owner).
		Scan(&raw, &native, &updateID, &replyLocale)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if err == nil {
		visible, visibleErr := b.historyReplyVisible(ctx, owner, updateID)
		if visibleErr != nil {
			return visibleErr
		}
		if !visible {
			raw, native = nil, false
		}
	}
	messages := orderMessages{language: language}
	notice := messages.text(i18n.OrdersTitle, nil)
	if len(raw) > 0 && (native || replyLocale == language) {
		if err = json.Unmarshal(raw, &notice); err != nil {
			return err
		}
	}
	// A view-only plan may have no answer. Telegram still requires nonempty
	// text on the menu that carries the navigation keyboard.
	if strings.TrimSpace(notice) == "" {
		notice, native = messages.text(i18n.OrdersTitle, nil), false
	}
	button, err := b.orderButton(
		ctx,
		owner,
		messages.text(i18n.OrderNew, nil),
		orders.Command{Name: actionCreateOrder, Choice: &orders.ChoiceInput{}},
	)
	if err != nil {
		return err
	}
	buttons := [][]telegram.Button{{button}}
	if canExport {
		exportButton, buttonError := b.orderButton(ctx, owner, "📥 XLSX", orders.Command{Name: actionExport})
		if buttonError != nil {
			return buttonError
		}
		buttons = append(buttons, []telegram.Button{exportButton})
	}
	if messages.err != nil {
		return messages.err
	}
	return b.deliverOrderCard(ctx, owner, "menu", telegram.FormatSend(telegram.Send{ChatID: chat, Text: notice,
		NativeMarkdown: native, Markup: telegram.Markup{Rows: buttons}}))
}

func (b *Bot) renderPaymentInbox(
	ctx context.Context,
	owner string,
	chat int64,
	event orders.Event,
	admins []orders.PaymentAdmin,
	active map[string]bool,
	available map[string]bool,
	language string,
) error {
	inbox, err := b.API.PaymentInbox(ctx, owner, event.ID)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status == http.StatusForbidden {
		return nil
	}
	if err != nil {
		return err
	}
	inbox, err = b.pagedOrders(ctx, owner, chat, adminOrdersScope, language, inbox, active, available)
	if err != nil {
		return err
	}
	for _, order := range inbox {
		payload, renderError := b.orderPayload(ctx, owner, chat, order, event, admins, true, language)
		if renderError != nil {
			return renderError
		}
		key := "admin:" + order.ID
		active[key] = true
		if err = b.deliverOrderCard(ctx, owner, key, payload); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) orderPayload(
	ctx context.Context,
	owner string,
	chat int64,
	order orders.Order,
	event orders.Event,
	admins []orders.PaymentAdmin,
	admin bool,
	language string,
) (telegram.Send, error) {
	amount, err := order.Choice.Total.MarshalJSON()
	if err != nil {
		return telegram.Send{}, err
	}
	messages := orderMessages{language: language}
	state, stateErr := i18n.Translate(language, orderStateID(order.State), nil)
	if stateErr != nil {
		return telegram.Send{}, stateErr
	}
	payload := telegram.Send{ChatID: chat, Text: messages.text(i18n.OrderSummary, map[string]string{
		mediaOrderChoice: order.ID,
		"state":          state,
		"version":        strconv.FormatInt(order.Version, 10),
		"total":          messages.number(string(amount)),
	}), Markup: telegram.Markup{Rows: [][]telegram.Button{}}}
	payload.Text += orderDescription(order, &messages)
	if !admin && (order.State == stateUnpaid || order.State == stateCash) && time.Now().Before(event.Deadline) {
		if _, complete := orderExtraControlKeys(order.Choice, event); !complete {
			payload.Text += "\n" + messages.text(i18n.OrderMoreControls, nil)
		}
	}
	if !admin && b.WebAppURL != "" {
		address, addressError := url.Parse(b.WebAppURL)
		if addressError != nil {
			return payload, addressError
		}
		query := address.Query()
		query.Set("order_id", order.ID)
		address.RawQuery = query.Encode()
		payload.Markup.Rows = append(
			payload.Markup.Rows,
			[]telegram.Button{
				{Text: messages.text(i18n.OrderMealsProfile, nil), WebApp: &telegram.WebApp{URL: address.String()}},
			},
		)
	}
	if admin {
		payload.Text = messages.text(
			i18n.OrderReviewTitle,
			nil,
		) + "\n" + payload.Text + "\n" + messages.text(
			i18n.OrderOwner,
			map[string]string{"owner": order.Owner},
		)
	}
	buttons := orderActions(order, event, admins, admin, &messages)
	for _, action := range buttons {
		if action.command.Name == actionInstructions {
			action.label, err = i18n.Translate(language, i18n.PaymentMethods, nil)
			if err != nil {
				return payload, err
			}
		}
		button, buttonError := b.orderButton(ctx, owner, action.label, action.command)
		if buttonError != nil {
			return payload, buttonError
		}
		payload.Markup.Rows = append(payload.Markup.Rows, []telegram.Button{button})
	}
	return payload, messages.err
}

const orderCardExtraLimit = 8
const orderCardExtraLabelRunes = 80
const orderInlineChoiceBytes = 16000

func orderDescription(order orders.Order, messages *orderMessages) string {
	keys := make([]string, 0, len(order.Choice.Extras))
	for key := range order.Choice.Extras {
		if key != orderTotalKey {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	var text strings.Builder
	if order.Choice.FirstName != "" || order.Choice.LastName != "" {
		text.WriteString(
			"\n" + messages.text(
				i18n.OrderCustomer,
				map[string]string{
					"first": fmt.Sprintf("%.120s", order.Choice.FirstName),
					"last":  fmt.Sprintf("%.120s", order.Choice.LastName),
				},
			),
		)
	}
	if len(order.Choice.Days) > 0 {
		text.WriteString("\n" + messages.mealDays(len(order.Choice.Days)))
	}
	shown := 0
	for _, key := range keys {
		if shown == orderCardExtraLimit || len([]rune(messages.extra(key))) > orderCardExtraLabelRunes {
			continue
		}
		price, _ := order.Choice.Extras[key].MarshalJSON()
		text.WriteString(
			"\n" + messages.text(
				i18n.OrderExtraPrice,
				map[string]string{
					orderExtraParameter: messages.extra(key),
					passPriceParameter:  messages.number(string(price)),
				},
			),
		)
		shown++
	}
	if shown < len(keys) {
		text.WriteString("\n" + messages.text(i18n.OrderMoreDetails, nil))
	}
	if order.State == stateCash {
		text.WriteString("\n" + messages.text(i18n.OrderCashPending, nil))
	}
	if order.Country != "" {
		text.WriteString(
			"\n" + messages.text(
				i18n.OrderPaymentContact,
				map[string]string{orderCountryParameter: strings.ToUpper(order.Country), "admin": order.PaymentAdmin},
			),
		)
	}
	if order.Country == "ru" {
		const rubPerBYN = 30
		const cents = 100
		rub := int64(order.Choice.Total) * rubPerBYN
		text.WriteString(
			"\n" + messages.text(
				i18n.OrderRUBTotal,
				map[string]string{"total": messages.number(fmt.Sprintf("%d.%02d", rub/cents, rub%cents))},
			),
		)
	}
	return text.String()
}

type orderAction struct {
	label   string
	command orders.Command
}

// Large choices stay available through the miniapp and full agent tools. Avoid
// persisting one full copy per inline toggle or ambiguous shortened labels.
func orderExtraControlKeys(choice orders.Choice, event orders.Event) ([]string, bool) {
	body, err := json.Marshal(choice)
	if err != nil || len(body) > orderInlineChoiceBytes {
		return nil, len(event.Extras) == 0
	}
	keys := make([]string, 0, len(event.Extras))
	complete := true
	for key, extra := range event.Extras {
		if extra.Legacy {
			continue
		}
		if len([]rune(key)) > orderCardExtraLabelRunes {
			complete = false
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	if len(keys) > orderCardExtraLimit {
		keys = keys[:orderCardExtraLimit]
		complete = false
	}
	return keys, complete
}

func orderActions(
	order orders.Order,
	event orders.Event,
	admins []orders.PaymentAdmin,
	admin bool,
	messages *orderMessages,
) []orderAction {
	base := orders.Command{OrderID: order.ID, Version: order.Version, Attempt: order.Attempt}
	if admin {
		accept, reject := base, base
		accept.Name, reject.Name = "accept", "reject"
		result := []orderAction{
			{messages.text(i18n.OrderAccept, nil), accept},
			{messages.text(i18n.OrderReject, nil), reject},
		}
		if order.State == stateProof {
			proof := base
			proof.Name = "show_proof"
			result = append([]orderAction{{messages.text(i18n.OrderOpenProof, nil), proof}}, result...)
		}
		return result
	}
	if order.State == stateProof {
		return orderProofActions(base, event, admins, messages)
	}
	if order.State != stateUnpaid && order.State != stateCash {
		return nil
	}
	proof := base
	proof.Name = "upload_proof"
	instructions := base
	instructions.Name = actionInstructions
	result := []orderAction{{"", instructions}}
	if !time.Now().Before(event.Deadline) {
		return result
	}
	if order.Choice.Total > 0 {
		result = append(result, orderAction{messages.text(i18n.OrderSendProof, nil), proof})
	}
	keys, _ := orderExtraControlKeys(order.Choice, event)
	for _, key := range keys {
		command := base
		command.Name = "edit"
		choice := choiceInput(order.Choice)
		label := messages.text(i18n.OrderAddExtra, map[string]string{orderExtraParameter: messages.extra(key)})
		if _, selected := choice.Extras[key]; selected {
			delete(choice.Extras, key)
			label = messages.text(i18n.OrderRemoveExtra, map[string]string{orderExtraParameter: messages.extra(key)})
		} else {
			choice.Extras[key] = json.RawMessage(`0`)
		}
		command.Choice = &choice
		result = append(result, orderAction{label, command})
	}
	for _, admin := range admins {
		if admin.Country == "be" && order.Choice.Total > 0 {
			command := base
			command.Name, command.PaymentAdmin = stateCash, admin.ID
			result = append(
				result,
				orderAction{messages.text(i18n.OrderCash, map[string]string{profileNameValue: admin.Name}), command},
			)
		}
	}
	base.Name = "delete"
	return append(result, orderAction{messages.text(i18n.OrderDelete, nil), base})
}

func orderProofActions(
	base orders.Command,
	event orders.Event,
	admins []orders.PaymentAdmin,
	messages *orderMessages,
) []orderAction {
	result := []orderAction{}
	for _, admin := range admins {
		command := base
		command.Name, command.Country, command.PaymentAdmin = "country", admin.Country, admin.ID
		result = append(
			result,
			orderAction{
				messages.text(
					i18n.OrderPayCountry,
					map[string]string{
						orderCountryParameter: strings.ToUpper(admin.Country),
						profileNameValue:      admin.Name,
					},
				),
				command,
			},
		)
	}
	base.Name = "cancel_proof"
	if !time.Now().Before(event.Deadline) {
		return result
	}
	return append(result, orderAction{messages.text(i18n.OrderCancelProof, nil), base})
}

func choiceInput(choice orders.Choice) orders.ChoiceInput {
	in := orders.ChoiceInput{
		Customer:   choice.Customer,
		FirstName:  choice.FirstName,
		LastName:   choice.LastName,
		Patronymic: choice.Patronymic,
		Days:       map[string]orders.DayInput{},
		Extras:     map[string]json.RawMessage{},
	}
	for key := range choice.Extras {
		if key != orderTotalKey {
			in.Extras[key] = json.RawMessage(`0`)
		}
	}
	for dayKey, day := range choice.Days {
		input := orders.DayInput{Mealtimes: map[string]orders.MealInput{}}
		for mealKey, meal := range day.Mealtimes {
			items := []orders.Item{}
			for _, item := range meal.Dishes {
				items = append(items, orders.Item{Name: item.Name, Count: item.Count})
			}
			input.Mealtimes[mealKey] = orders.MealInput{Dishes: items}
		}
		in.Days[dayKey] = input
	}
	return in
}
