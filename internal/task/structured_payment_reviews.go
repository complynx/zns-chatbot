package task

import (
	"context"

	"github.com/complynx/zns-chatbot/internal/db"
)

func structuredFormLabel(command string, ru bool) string {
	if command == menuCommand || command == startCommand {
		return structuredText(ru, "Open meals", "Открыть питание")
	}
	if command == paymentsCommand {
		return structuredText(ru, "Open pass payment", "Открыть оплату пасса")
	}
	if command == pairsCommand {
		return structuredText(ru, "Open pair invitations", "Открыть приглашения в пару")
	}
	if command == avatarCommand {
		return structuredText(ru, "Open avatar", "Открыть аватарку")
	}
	if command == paymentReviewsCommand {
		return structuredText(ru, "Review payments", "Проверить платежи")
	}
	if command == massageCommand {
		return structuredText(ru, "Open massage bookings", "Открыть записи на массаж")
	}
	return structuredText(ru, "Open saved selection", "Открыть сохранённый выбор")
}

func (s *Service) structuredPaymentReviews(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	scope db.PaymentScope,
	ru bool,
) error {
	allowed, err := s.store.HasPaymentAuthority(ctx, scope)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	text := structuredText(
		ru,
		"Payment review requires current event reviewer access.",
		"Для проверки платежей нужны текущие права амбассадора или администратора эвента.",
	)
	url := ""
	if allowed {
		if s.options.MiniAppURL == "" {
			text = structuredText(
				ru,
				"The payment-review form is currently unavailable. Try /payment_reviews later.",
				"Форма проверки платежей сейчас недоступна. Попробуй /payment_reviews позже.",
			)
		} else {
			text = structuredText(
				ru,
				"Open the form to inspect proof and prepare a decision. Payment is confirmed only after your explicit confirmation.",
				"Открой форму для проверки файла и подготовки решения. Оплата подтверждается только после твоего явного подтверждения.",
			)
			url = s.options.MiniAppURL + "#payment-review"
		}
	}
	t.State = stateCompleted
	if err = s.save(ctx, t, c, structuredOutput(i, text, nil, ru, url)); err != nil {
		return s.wait()
	}
	return s.deliver(ctx, t, c)
}
