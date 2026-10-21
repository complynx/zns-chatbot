package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Loom receives a bounded summary; the bot's dedicated approval reads the exact
// preparation and presents every frozen meal component and charge before consent.
func mealOrderSubmissionDescription(p db.OrderPreparation, language string) (string, error) {
	zone, err := time.LoadLocation(p.Snapshot.Quote.Timezone)
	if err != nil {
		return "", err
	}
	template := `Submit this exact saved meal draft?
Event: %s
Draft revision: %d; event/catalog versions: %d/%d
Total: %s
Payment proof / cancellation deadline: %s (%s)
The bot must show the complete frozen composition and all prices before approval.
Submission clears this exact draft, creates no capacity reservation and confirms no payment.`
	if language == "ru" {
		template = `Оформить этот сохранённый заказ питания?
Эвент: %s
Ревизия черновика: %d; версии эвента/меню: %d/%d
Итого: %s
Срок подтверждения оплаты / отмены: %s (%s)
Перед согласием бот покажет полный сохранённый состав и все цены.
Оформление очищает этот черновик, не резервирует места и не подтверждает оплату.`
	}
	q := p.Snapshot.Quote
	text := fmt.Sprintf(
		template,
		strconv.Quote(p.Snapshot.EventName),
		p.Arguments.ExpectedDraftVersion,
		p.Arguments.ExpectedEventVersion,
		p.Arguments.ExpectedCatalogVersion,
		orderMoney(
			q.TotalMinor,
			q.CurrencyScale,
			q.Currency,
		),
		p.Snapshot.SalesClosesAt.In(zone).Format(time.RFC3339),
		q.Timezone,
	)
	fence := preparationCodeFence
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "\n" + text + "\n" + fence, nil
}
