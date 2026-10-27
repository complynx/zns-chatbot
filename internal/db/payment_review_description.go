package db

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const paymentReviewSummaryEnglish = `Event: %s
Payer: %s
Credit: %s
Reported amount: %s
Remaining before this decision: %s
Remaining after this decision: %s
Required deposit: %s
Deposit already satisfied: %s
Proof sent: %s
Evidence deadline: %s (%s)
Sent before deadline: %s
Reason: %s
Proof ID: %s
This decision requires your approval.`

const paymentReviewSummaryRussian = `Эвент: %s
Плательщик: %s
Зачислить: %s
Заявленная сумма: %s
Остаток до этого решения: %s
Остаток после этого решения: %s
Необходимая предоплата: %s
Предоплата уже зачтена: %s
Файл отправлен: %s
Срок отправки подтверждения: %s (%s)
Отправлено до дедлайна: %s
Причина: %s
ID подтверждения: %s
Для этого решения нужно твоё согласие.`

func paymentReviewLanguage(p sqlc.ReadPaymentReviewPresentationRow) string {
	return reviewLanguage(p.LanguagePreference, p.TelegramLanguage, p.DefaultLanguage)
}

func reviewLanguage(preference, telegram, defaultLanguage string) string {
	if preference == "ru" || preference == "en" {
		return preference
	}
	code := strings.TrimSpace(telegram)
	if code == "" {
		code = defaultLanguage
	}
	base := strings.ToLower(strings.Split(strings.ReplaceAll(code, "_", "-"), "-")[0])
	if base == "ru" || base == "be" || base == "uk" {
		return "ru"
	}
	return "en"
}

// Inputs use the validated frozen pass currency and scale, never floating point.
func paymentReviewMoney(amount int64, scale int32, currency string) string {
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

func paymentReviewDescription(args payments.ReviewChange, target paymentReviewTarget,
	p sqlc.ReadPaymentReviewPresentationRow, zone *time.Location,
) string {
	ru := paymentReviewLanguage(p) == "ru"
	title, template, yes, no := "Confirm payment?", paymentReviewSummaryEnglish, "yes", "no"
	if args.Decision == payments.Rejected {
		title = "Reject payment proof?"
	}
	if ru {
		title, template, yes, no = "Подтвердить оплату?", paymentReviewSummaryRussian, "да", "нет"
		if args.Decision == payments.Rejected {
			title = "Отклонить подтверждение оплаты?"
		}
	}
	answer := func(value bool) string {
		if value {
			return yes
		}
		return no
	}
	money := func(value int64) string {
		return paymentReviewMoney(value, target.Pass.CurrencyScale, target.Pass.Currency)
	}
	body := fmt.Sprintf(template,
		strconv.Quote(p.EventName), strconv.Quote(p.PayerName), money(args.AmountMinor),
		money(target.Evidence.AmountMinor), money(target.Balance.RemainingMinor),
		money(target.Balance.RemainingMinor-args.AmountMinor), money(target.Balance.DepositRequiredMinor),
		answer(target.Balance.DepositSatisfied), target.Evidence.SentAt.In(zone).Format(time.RFC3339),
		target.Evidence.DueAt.In(zone).Format(time.RFC3339), target.Timezone,
		answer(target.Evidence.Timely), strconv.Quote(args.Reason), args.EvidenceID)
	// Proof metadata is literal data, including embedded Markdown and line breaks.
	fence := "```"
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return title + "\n\n" + fence + "\n" + body + "\n" + fence
}
