package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const preparationCodeFence = "```"

func (s *Server) orderPresentationLanguage(ctx context.Context, scope db.ProfileScope) (string, error) {
	profile, err := s.store.GetProfile(ctx, scope)
	if err != nil {
		return "", err
	}
	if profile.BotLanguage == "ru" || profile.BotLanguage == "en" {
		return profile.BotLanguage, nil
	}
	user, err := s.store.GetUser(ctx, scope.UserID)
	if err != nil {
		return "", err
	}
	var metadata telegram.User
	if err = json.Unmarshal(user.Profile, &metadata); err != nil {
		return "", err
	}
	language := strings.TrimSpace(metadata.LanguageCode)
	if language == "" {
		settings, settingsErr := s.store.Settings(ctx)
		if settingsErr != nil {
			return "", settingsErr
		}
		language = settings.Document.DefaultLanguage
	}
	base := strings.ToLower(strings.Split(strings.ReplaceAll(language, "_", "-"), "-")[0])
	if base == "ru" || base == "be" || base == "uk" {
		return "ru", nil
	}
	return "en", nil
}

// The full price breakdown is read through selection before approval. This
// bounded summary stays within Loom's 4000-byte preparation description contract.
func orderSubmissionDescription(p db.OrderPreparation, language string) (string, error) {
	zone, err := time.LoadLocation(p.Snapshot.Quote.Timezone)
	if err != nil {
		return "", err
	}
	q := p.Snapshot.Quote
	total := orderMoney(q.TotalMinor, q.CurrencyScale, q.Currency)
	title := "Submit order?"
	template := `Event: %s
Saved draft revision: %d
Event/catalog versions: %d/%d
Item lines: %d; additional charge lines: %d
Total: %s
Sales cutoff: %s (%s)
Review the full saved selection and all additional charges before approval.
Submission clears this exact draft. It reserves no capacity and confirms no money.`
	if language == "ru" {
		title = "Оформить заказ?"
		template = `Эвент: %s
Ревизия сохранённого черновика: %d
Версии эвента/каталога: %d/%d
Строк позиций: %d; строк доплат: %d
Итого: %s
Закрытие продаж: %s (%s)
Перед согласием проверь полный сохранённый состав и все доплаты.
Оформление очищает именно этот черновик. Места не резервируются, оплата не подтверждается.`
	}
	body := fmt.Sprintf(
		template,
		strconv.Quote(p.Snapshot.EventName),
		p.Arguments.ExpectedDraftVersion,
		p.Arguments.ExpectedEventVersion,
		p.Arguments.ExpectedCatalogVersion,
		len(q.Items),
		len(q.Supplements),
		total,
		p.Snapshot.SalesClosesAt.In(zone).Format(time.RFC3339),
		q.Timezone,
	)
	fence := preparationCodeFence
	for strings.Contains(body, fence) {
		fence += "`"
	}
	return title + "\n\n" + fence + "\n" + body + "\n" + fence, nil
}

func orderMoney(amount int64, scale int, currency string) string {
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

// Loom caps preparation descriptions at4000 bytes. The bot's specialized review
// presentation reads the complete immutable receipt before the consent keyboard;
// only this transport description is abbreviated, never the stored consent.
const maxOrderReviewPrepareDescriptionBytes = 4000

func orderReviewPrepareDescription(p db.OrderReviewCommitPreparation) string {
	if len(p.Description) <= maxOrderReviewPrepareDescriptionBytes {
		return p.Description
	}
	return fmt.Sprintf(
		"Review exact saved order payment decision.\nEvent: %s\nEvidence: %s\nWorksp" +
"ace: %s\nRevision: %d\nDecision: %s\nThe full immutable review, including m" +
"eal composition, prices, proof timing and reason, is retained by the bot" +
" and must be displayed before human approval. This short tool descriptio" +
"n is not the complete consent presentation.",
		p.Arguments.Review.EventID,
		p.Arguments.Review.EvidenceID,
		p.Arguments.WorkspaceID,
		p.Arguments.Revision,
		p.Arguments.Review.Decision,
	)
}
