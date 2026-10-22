package task

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/orders"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const orderSubmitApprovalMethod = "zinusya_orders.submit"

func (s *Service) pendingApprovalMessages(
	ctx context.Context,
	t *db.Task,
	i intent,
	a *loom.Approval,
) ([]db.Outbound, error) {
	if a == nil || a.Decision != "" {
		return nil, nil
	}
	t.State, t.QuestionID = "awaiting_user", a.ID
	if !a.ExpiresAt.After(time.Now()) {
		return nil, nil
	}
	if a.Method == "zinusya_pairs.assign" {
		text, err := s.pairAssignmentApprovalText(ctx, *t, i, *a)
		if err != nil {
			return nil, err
		}
		return s.approvalContent(ctx, i, t.ID, *a, text), nil
	}
	if a.Method == "zinusya_payments.review_commit" {
		text, err := s.paymentReviewApprovalText(ctx, *t, i, *a)
		if err != nil {
			return nil, err
		}
		return s.approvalContent(ctx, i, t.ID, *a, text), nil
	}
	if a.Method == "zinusya_order_payments.review_commit" {
		text, err := s.orderReviewApprovalText(ctx, *t, i, *a)
		if err != nil {
			return nil, err
		}
		return s.approvalContent(ctx, i, t.ID, *a, text), nil
	}
	if a.Method != orderSubmitApprovalMethod && a.Method != "zinusya_meals.submit" {
		return s.approval(ctx, i, t.ID, *a), nil
	}
	text, nativeURL, err := s.orderApprovalText(ctx, *t, i, *a)
	if err != nil {
		return nil, err
	}
	out := s.approvalContent(ctx, i, t.ID, *a, text)
	if nativeURL == "" || len(out) == 0 {
		return out, nil
	}
	label := "Continue in form"
	if s.languageFor(ctx, i) == "ru" {
		label = "Продолжить в форме"
	}
	// Reply-keyboard launches have no initData. Keep the conversational keyboard
	// on the final quote message and use a separate durable inline WebApp button.
	form := s.result(i, label, "approval:dialog-v1:"+keyboardPromptKey(*a)+":form")
	var payload telegram.SendMessageRequest
	if err = json.Unmarshal(form[0].Payload, &payload); err != nil {
		return nil, err
	}
	payload.ReplyMarkup = &telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{
		Text: label, WebApp: &telegram.WebAppInfo{URL: nativeURL},
	}}}}
	form[0].Payload, err = json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	recordPromptContext(form, loom.InputPresentation{ApprovalID: a.ID})
	return append(form, out...), nil
}

// Order approval uses the immutable preparation receipt, not the mutable draft
// or Loom's bounded description. The normal outbox splits all lines before consent.
func (s *Service) orderApprovalText(ctx context.Context, t db.Task, i intent, a loom.Approval) (string, string, error) {
	operation, err := uuid.Parse(a.OperationID)
	if err != nil || operation == uuid.Nil {
		return "", "", errors.New("invalid order approval operation")
	}
	var args db.OrderSubmission
	decoder := json.NewDecoder(bytes.NewReader(a.Arguments))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&args); err != nil {
		return "", "", err
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return "", "", errors.New("trailing order approval arguments")
	}
	user, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		return "", "", err
	}
	bot, organization, _ := s.iam.DeploymentScope()
	if user.BotID != bot || user.TelegramUserID != i.User.ID {
		return "", "", errors.New("order approval recipient mismatch")
	}
	scope := db.ProfileScope{BotID: bot, Organization: organization, Subject: user.Subject, UserID: user.ID}
	var receipt db.OrderOperation
	switch a.Method {
	case orderSubmitApprovalMethod:
		receipt, err = s.store.OwnOrderOperation(ctx, scope, args.EventID, operation, db.OrderPrepare)
	case "zinusya_meals.submit":
		receipt, err = s.store.OwnMealOrderOperation(ctx, scope, args.EventID, operation, db.OrderPrepare)
	default:
		return "", "", errors.New("unsupported order approval method")
	}
	if err != nil {
		return "", "", err
	}
	var prepared db.OrderPreparation
	if err = json.Unmarshal(receipt.Result, &prepared); err != nil {
		return "", "", err
	}
	if prepared.Arguments != args || a.ExpiresAt.After(prepared.ExpiresAt) {
		return "", "", errors.New("order approval does not match preparation")
	}
	text, err := fullOrderApproval(prepared, a.ExpiresAt, s.languageFor(ctx, i))
	nativeURL := ""
	if err == nil && a.Method == orderSubmitApprovalMethod && s.options.MiniAppURL != "" && i.ChatID == i.User.ID {
		nativeURL = s.options.MiniAppURL + "#orders?event=" + prepared.Arguments.EventID.String() + "&operation=" + receipt.OperationID.String()
	}
	return text, nativeURL, err
}

func fullOrderApproval(p db.OrderPreparation, expires time.Time, language string) (string, error) {
	q := p.Snapshot.Quote
	zone, err := time.LoadLocation(q.Timezone)
	if err != nil {
		return "", err
	}
	ru := language == "ru"
	title, event, items, charges, total, cutoff, expiry, each := "Submit order?", "Event", "Items", "Required charges", "Total", "Sales/cancellation cutoff", "Confirm before", "each"
	note := "Submission clears this exact saved draft. It reserves no capacity and confirms no money."
	if ru {
		title, event, items, charges, total, cutoff, expiry, each = "Оформить заказ?", "Эвент", "Позиции", "Обязательные доплаты", "Итого", "Закрытие продаж/срок отмены", "Подтвердить до", "за единицу"
		note = "Оформление очищает именно этот сохранённый выбор. Места не резервируются, оплата не подтверждается."
	}
	var body strings.Builder
	fmt.Fprintf(&body, "%s: %s\n", event, strconv.Quote(p.Snapshot.EventName))
	if meal := p.Snapshot.Meal; meal != nil {
		composition, presentationErr := meal.Presentation(language)
		if presentationErr != nil {
			return "", presentationErr
		}
		body.WriteString(composition)
		cutoff = "Payment proof / cancellation deadline"
		if ru {
			cutoff = "Срок подтверждения оплаты / отмены"
		}
	} else {
		writeOrderServices(&body, p.Snapshot.Services, zone, ru)
		writeOrderCapacity(&body, p.Snapshot.Capacity, ru)
	}
	writeLines := func(heading string, lines []orders.QuoteLine) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&body, "\n%s:\n", heading)
		for _, line := range lines {
			label := line.Name.EN
			if ru {
				label = line.Name.RU
			}
			fmt.Fprintf(
				&body,
				"%s (%s/%s) × %d\n  %s %s = %s\n",
				strconv.Quote(label),
				line.OfferID,
				line.ID,
				line.Quantity,
				orderApprovalMoney(line.UnitPriceMinor, q.CurrencyScale, q.Currency),
				each,
				orderApprovalMoney(line.AmountMinor, q.CurrencyScale, q.Currency),
			)
		}
	}
	if p.Snapshot.Meal == nil {
		writeLines(items, q.Items)
		writeLines(charges, q.Supplements)
	}
	fmt.Fprintf(
		&body,
		"\n%s: %s\n%s: %s (%s)\n%s: %s (%s)\n\n%s",
		total,
		orderApprovalMoney(q.TotalMinor, q.CurrencyScale, q.Currency),
		cutoff,
		p.Snapshot.SalesClosesAt.In(zone).Format(time.RFC3339),
		q.Timezone,
		expiry,
		expires.In(zone).Format(time.RFC3339),
		q.Timezone,
		note,
	)
	text := body.String()
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return title + "\n\n" + fence + "\n" + text + "\n" + fence, nil
}

func writeOrderServices(body *strings.Builder, services []db.OrderService, zone *time.Location, ru bool) {
	if len(services) == 0 {
		return
	}
	heading := "Services"
	if ru {
		heading = "Услуги"
	}
	fmt.Fprintf(body, "\n%s:\n", heading)
	for _, service := range services {
		label := service.Name.EN
		if ru {
			label = service.Name.RU
		}
		fmt.Fprintf(body, "%s (%s)\n  %s — %s (%s)\n", strconv.Quote(label), service.OfferID,
			service.StartsAt.In(zone).Format(time.RFC3339), service.EndsAt.In(zone).Format(time.RFC3339), zone.String())
	}
}

func orderApprovalMoney(amount int64, scale int, currency string) string {
	divisor := int64(1)
	for range scale {
		divisor *= 10
	}
	value := strconv.FormatInt(amount, 10)
	if scale > 0 {
		value = fmt.Sprintf("%d.%0*d", amount/divisor, scale, amount%divisor)
	}
	return value + " " + currency
}

func writeOrderCapacity(body *strings.Builder, capacity *orders.CapacitySnapshot, ru bool) {
	if capacity == nil {
		if ru {
			body.WriteString("\nТребования к ограниченным ресурсам не настроены.\n")
		} else {
			body.WriteString("\nLimited resource requirements are not configured.\n")
		}
		return
	}
	heading := "Limited resource requirements"
	if ru {
		heading = "Требования к ограниченным ресурсам"
	}
	fmt.Fprintf(body, "\n%s:\n", heading)
	if len(capacity.Demands) == 0 {
		if ru {
			body.WriteString("Ограниченные ресурсы не требуются.\n")
		} else {
			body.WriteString("No limited resources required.\n")
		}
	}
	for _, demand := range capacity.Demands {
		label := demand.Name.EN
		if ru {
			label = demand.Name.RU
		}
		fmt.Fprintf(body, "%s (%s): %d\n", strconv.Quote(label), demand.ResourceID, demand.Units)
	}
}
