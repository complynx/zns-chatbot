package db

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const orderReviewSummaryEnglish = `Event: %s
Payer: %s
Order: %s
Reported amount: %s
Frozen order total: %s
Amount to confirm: %s
Proof sent: %s
Proof linked to order: %s
Evidence deadline: %s (%s)
Approve before: %s (%s)
Reason: %s
Proof ID: %s

%s`

const orderReviewSummaryRussian = `Эвент: %s
Плательщик: %s
Заказ: %s
Заявленная сумма: %s
Зафиксированная стоимость заказа: %s
Подтвердить сумму: %s
Файл отправлен: %s
Подтверждение связано с заказом: %s
Срок отправки подтверждения: %s (%s)
Подтвердить решение до: %s (%s)
Причина: %s
ID подтверждения: %s

%s`

// Called only inside the authorized event transaction. Sold terms come from the
// immutable order, not today's catalog/event settings. Payer identity is literal.
func orderReviewSummary(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	p OrderEvidenceReviewPreparation,
	language string,
) (string, error) {
	evidence := p.Evidence
	row, err := q.ReadSubmittedOrder(
		ctx,
		sqlc.ReadSubmittedOrderParams{ID: evidence.OrderID, EventID: evidence.EventID, UserID: evidence.UserID},
	)
	if err != nil {
		return "", err
	}
	order, err := submittedOrderRecord(row)
	if err != nil {
		return "", err
	}
	quote := order.Snapshot.Quote
	if quote.CurrencyScale < 0 || quote.CurrencyScale > 6 || quote.TotalMinor != evidence.QuotedAmountMinor ||
		quote.Currency != evidence.Currency ||
		quote.CurrencyScale != evidence.CurrencyScale {
		return "", ErrConflict
	}
	presentation, err := q.ReadOrderReviewPresentation(
		ctx,
		sqlc.ReadOrderReviewPresentationParams{
			ActorID:      scope.UserID,
			EvidenceID:   evidence.ID,
			EventID:      evidence.EventID,
			BotID:        scope.BotID,
			Organization: scope.Organization,
			Subject:      scope.Subject,
		},
	)
	if err != nil {
		return "", err
	}
	if language == "" || language == profileLanguageAuto {
		language = reviewLanguage(
			presentation.LanguagePreference,
			presentation.TelegramLanguage,
			presentation.DefaultLanguage,
		)
	}
	zone, err := time.LoadLocation(quote.Timezone)
	if err != nil {
		return "", err
	}
	return renderOrderReviewSummary(p, order.Snapshot, presentation.PayerName, zone, language == "ru"), nil
}

func renderOrderReviewSummary(
	p OrderEvidenceReviewPreparation,
	snapshot OrderSnapshot,
	payer string,
	zone *time.Location,
	ru bool,
) string {
	e := p.Evidence
	template, title, note := orderReviewSummaryEnglish, "Confirm order payment?", "Records confirmed payment and occupies this proof's held resources. It does not fulfill the order."
	confirmed := e.QuotedAmountMinor
	if p.Arguments.Decision == OrderEvidenceRejected {
		title = "Reject order payment proof?"
		confirmed = 0
		note = "Rejects this proof and releases only its held resources, if any. It does not refund money or cancel the order."
	}
	if ru {
		template, title, note = orderReviewSummaryRussian, "Подтвердить оплату заказа?", "Фиксирует подтверждённую оплату и занимает зарезервированные по этому подтверждению места. Это не выполнение заказа."
		if p.Arguments.Decision == OrderEvidenceRejected {
			title = "Отклонить подтверждение оплаты заказа?"
			note = "Отклоняет это подтверждение и освобождает только его резерв, если он был. Деньги не возвращаются, заказ не отменяется."
		}
	}
	money := func(amount int64) string { return paymentReviewMoney(amount, int32(e.CurrencyScale), e.Currency) } // #nosec G115 -- orderReviewSummary validates scale0..6 before calling this renderer.
	body := fmt.Sprintf(
		template,
		strconv.Quote(snapshot.EventName),
		strconv.Quote(payer),
		e.OrderID,
		money(e.AmountMinor),
		money(e.QuotedAmountMinor),
		money(confirmed),
		e.SentAt.In(zone).Format(time.RFC3339),
		e.SubmittedAt.In(zone).Format(time.RFC3339),
		e.DueAt.In(zone).Format(time.RFC3339),
		zone.String(),
		p.ExpiresAt.In(zone).Format(time.RFC3339),
		zone.String(),
		strconv.Quote(p.Arguments.Reason),
		e.ID,
		note,
	)
	var details strings.Builder
	writeOrderReviewLines(
		&details,
		snapshot.Quote.Items,
		false,
		ru,
		snapshot.Quote.CurrencyScale,
		snapshot.Quote.Currency,
	)
	writeOrderReviewLines(
		&details,
		snapshot.Quote.Supplements,
		true,
		ru,
		snapshot.Quote.CurrencyScale,
		snapshot.Quote.Currency,
	)
	for _, service := range snapshot.Services {
		label := service.Name.EN
		if ru {
			label = service.Name.RU
		}
		fmt.Fprintf(
			&details,
			"\n%s: %s — %s (%s)\n",
			strconv.Quote(label),
			service.StartsAt.In(zone).Format(time.RFC3339),
			service.EndsAt.In(zone).Format(time.RFC3339),
			zone.String(),
		)
	}
	writeOrderReviewCapacity(&details, snapshot.Capacity, e.AdmissionState, ru)
	body += details.String()
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return title + "\n\n" + fence + "\n" + body + "\n" + fence
}

func writeOrderReviewLines(
	body *strings.Builder,
	lines []orders.QuoteLine,
	supplements, ru bool,
	scale int,
	currency string,
) {
	if len(lines) == 0 {
		return
	}
	title := "Items"
	if supplements {
		title = "Required charges"
	}
	if ru {
		title = "Позиции"
		if supplements {
			title = "Обязательные доплаты"
		}
	}
	fmt.Fprintf(body, "\n%s:\n", title)
	for _, line := range lines {
		name := line.Name.EN
		if ru {
			name = line.Name.RU
		}
		fmt.Fprintf(
			body,
			"%s: %s × %d = %s\n",
			strconv.Quote(name),
			paymentReviewMoney(line.UnitPriceMinor, int32(scale), currency),
			line.Quantity,
			paymentReviewMoney(line.AmountMinor, int32(scale), currency),
		) // #nosec G115 -- scale comes from the validated frozen quote0..6.
	}
}

func writeOrderReviewCapacity(body *strings.Builder, capacity *orders.CapacitySnapshot, state string, ru bool) {
	if state != OrderEvidencePending {
		if ru {
			body.WriteString("\nПри приёме этого подтверждения места не резервировались.\n")
		} else {
			body.WriteString("\nNo capacity was held when this proof was accepted.\n")
		}
		return
	}
	heading := "Capacity held for this proof"
	if ru {
		heading = "Резерв по этому подтверждению"
	}
	fmt.Fprintf(body, "\n%s:\n", heading)
	if capacity == nil || len(capacity.Demands) == 0 {
		if ru {
			body.WriteString("Ограниченные ресурсы не требуются.\n")
		} else {
			body.WriteString("No limited resources required.\n")
		}
		return
	}
	for _, demand := range capacity.Demands {
		name := demand.Name.EN
		if ru {
			name = demand.Name.RU
		}
		fmt.Fprintf(body, "%s: %d\n", strconv.Quote(name), demand.Units)
	}
}
