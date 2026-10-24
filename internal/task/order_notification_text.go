package task

import (
	"errors"
	"fmt"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

// Order notice text is literal data, not Markdown, and describes the retained
// business event. Human confirmation is independent of Telegram delivery.
func orderNotificationText(n db.OrderNotification, language string, reviewer bool) (string, error) {
	e := n.Evidence
	if e.CurrencyScale < 0 || e.CurrencyScale > orders.MaxCurrencyScale {
		return "", errors.New("invalid order notice currency scale")
	}
	zoneName := n.Order.Snapshot.Quote.Timezone
	zone, err := time.LoadLocation(zoneName)
	if err != nil {
		return "", err
	}
	ru := language == "ru"
	status, err := orderNoticeStatus(n, ru, reviewer)
	if err != nil {
		return "", err
	}
	sent := e.SentAt.In(zone).Format(time.RFC3339)
	due := e.DueAt.In(zone).Format(time.RFC3339)
	amount := noticeAmount(e.AmountMinor, int32(e.CurrencyScale), e.Currency)
	title := structuredText(ru, "Order payment update", "Оплата заказа")
	if e.OrderKind == db.OrderKindMeal {
		title = structuredText(ru, "Meal payment update", "Оплата питания")
	}
	text := fmt.Sprintf("%s: %.200s\n%s\n%s: %s\n%s: %s\n%s: %s (%s)\n%s: %s\n%s: %s\n%s: %s",
		title, n.Order.Snapshot.EventName, status,
		structuredText(ru, "Reported amount", "Заявленная сумма"), amount,
		structuredText(ru, "Proof sent", "Файл отправлен"), sent,
		structuredText(ru, "Evidence deadline", "Срок отправки подтверждения"), due, zoneName,
		structuredText(ru, "Event", "Эвент"), e.EventID,
		structuredText(ru, "Order", "Заказ"), e.OrderID,
		structuredText(ru, "Evidence", "Доказательство"), e.ID)
	if e.Review != nil {
		text += fmt.Sprintf(
			"\n%s: %s (%s)\n%s: %s",
			structuredText(
				ru,
				"Decision time",
				"Время решения",
			),
			e.Review.ReviewedAt.In(zone).Format(time.RFC3339),
			zoneName,
			structuredText(ru, "Decision reason", "Причина решения"),
			e.Review.Reason,
		)
	}
	return text, nil
}

func orderNoticeStatus(n db.OrderNotification, ru, reviewer bool) (string, error) {
	switch n.Kind {
	case db.OrderEvidencePending:
		if n.Evidence.OrderKind == db.OrderKindMeal {
			return structuredText(
				ru,
				"Meal payment awaits human review. Meal quantities are unlimited; no capacity is held.",
				"Оплата питания ожидает проверки человеком. Количество порций не ограничено; места не резервируются.",
			), nil
		}
		return structuredText(
			ru,
			"Proof received. Payment awaits human review; limited resources are held for this evidence.",
			"Подтверждение получено. Оплата ожидает проверки человеком; ограниченные "+
				"ресурсы удерживаются за этим подтверждением.",
		), nil
	case db.OrderEvidenceExhausted:
		if reviewer {
			return structuredText(
				ru,
				"Capacity is exhausted. Check the payment; a refund requires attention. N"+
					"o resources are held and no refund was performed.",
				"Места закончились. Проверьте платёж: требуется решение о возврате. Ресурсы не удерживаются, возврат не выполнен.",
			), nil
		}
		return structuredText(
			ru,
			"Places have run out. An event ambassador must review the payment and res"+
				"olve the refund. No places are held and no automatic refund was made.",
			"Места закончились. Амбассадор эвента проверит платёж и решит вопрос возв"+
				"рата. Места не удерживаются, автоматический возврат не выполнялся.",
		), nil
	case db.OrderEvidenceNeedsDecision:
		return structuredText(
			ru,
			"A human case decision is required. Payment is not confirmed and no resources are held.",
			"Требуется решение человека по этому случаю. Оплата не подтверждена, ресурсы не удерживаются.",
		), nil
	case db.OrderEvidenceConfirmed:
		if n.Evidence.Review == nil {
			return "", errors.New("order decision notice has no review")
		}
		if n.Evidence.OrderKind == db.OrderKindMeal {
			return structuredText(
				ru,
				"Meal payment confirmed by the reviewer.",
				"Оплата питания подтверждена проверяющим.",
			), nil
		}
		return structuredText(
			ru,
			"Order payment confirmed by the reviewer. Held resources are now occupied.",
			"Оплата заказа подтверждена проверяющим. Удерживаемые ресурсы заняты заказом.",
		), nil
	case db.OrderEvidenceRejected:
		if n.Evidence.Review == nil {
			return "", errors.New("order decision notice has no review")
		}
		if n.Evidence.OrderKind == db.OrderKindMeal {
			return structuredText(
				ru,
				"Meal payment evidence rejected. The order was not cancelled and no refund was performed.",
				"Подтверждение оплаты питания отклонено. Заказ не отменён, возврат не выполнен.",
			), nil
		}
		return structuredText(
			ru,
			"Payment evidence rejected. Its holds were released; the order was not cancelled and no refund was performed.",
			"Подтверждение оплаты отклонено. Его резервы освобождены; заказ не отменён, возврат не выполнен.",
		), nil
	default:
		return "", errors.New("invalid order notice kind")
	}
}
