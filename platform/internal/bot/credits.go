package bot

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const creditsUsageCommand = "/usage"

const creditOwnerWords = 2
const creditPolicyWords = 3
const creditFractionDigits = 9

func paidFailureNotice(err error) i18n.ID {
	if errors.Is(err, credits.ErrLimit) {
		return i18n.BillingLimit
	}
	if errors.Is(err, credits.ErrUnpriced) {
		return i18n.BillingUnpriced
	}
	return i18n.AgentUnavailable
}

func isCreditsUpdate(text string) bool {
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case creditsUsageCommand, "/credits_user", "/credits_policy", "/credits_default":
		return true
	default:
		return false
	}
}
func (b *Bot) handleCredits(ctx context.Context, in incoming, u telegram.Update) error {
	prefs, err := b.API.Preferences(ctx, in.owner)
	if err != nil {
		return err
	}
	messages := &orderMessages{language: prefs.Language}
	text, err := b.creditsCommand(ctx, in, u, messages)
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < 500 {
		text = messages.text(i18n.BillingDenied, nil)
		err = nil
	}
	if err != nil {
		return err
	}
	if messages.err != nil {
		return messages.err
	}
	ref := botdelivery.Reference{Family: botFamilyCredits}
	result := botdelivery.StoredResult{Payload: telegram.Send{ChatID: in.chat, Text: text}}
	if parts := strings.Fields(in.text); len(parts) > 0 && parts[0] != creditsUsageCommand {
		ref.Object = "admin"
	}
	if text == messages.text(i18n.BillingDenied, nil) {
		ref.Family = botFamilyStatic
		result.Notice = i18n.BillingDenied
	}
	if err = b.queueBotResult(ctx, in.owner, in.chat, u.ID, botFamilyCredits, ref, result, 0); err != nil {
		return err
	}
	return b.record(ctx, in.owner, u.ID, botFamilyCredits, map[string]string{"text": text})
}
func (b *Bot) creditsCommand(ctx context.Context, in incoming, u telegram.Update, m *orderMessages) (string, error) {
	parts := strings.Fields(in.text)
	endpoint := "/v1/credits"
	if parts[0] != creditsUsageCommand {
		var permissions map[string]bool
		if err := b.API.Call(ctx, in.owner, http.MethodGet, endpoint+"/permissions", nil, &permissions); err != nil {
			return "", err
		}
		if !permissions["admin"] {
			return m.text(i18n.BillingDenied, nil), nil
		}
		if parts[0] == "/credits_default" {
			return b.creditDefaultCommand(ctx, in, u, m, parts)
		}
		if len(parts) < creditOwnerWords {
			return m.text(i18n.BillingPolicyHelp, nil), nil
		}
		endpoint += "/users/" + url.PathEscape(parts[1])
	}
	var report credits.UsageReport
	if err := b.API.Call(ctx, in.owner, http.MethodGet, endpoint+creditsUsageCommand, nil, &report); err != nil {
		return "", err
	}
	if parts[0] == "/credits_policy" {
		if len(parts) != creditPolicyWords {
			return m.text(i18n.BillingPolicyHelp, nil), nil
		}
		change, valid := creditPolicyChange(parts[2], report.Policy.Version, u.ID)
		if !valid {
			return m.text(i18n.BillingPolicyHelp, nil), nil
		}
		var updated credits.Policy
		if err := b.API.Call(ctx, in.owner, http.MethodPost, endpoint+"/policy", change, &updated); err != nil {
			return "", err
		}
		return m.text(i18n.BillingSaved, nil), nil
	}
	return creditUsageMessage(m, report), nil
}

func creditUsageMessage(m *orderMessages, report credits.UsageReport) string {
	available := m.text(i18n.BillingUnlimited, nil)
	allowance := creditAmount(report.Policy.MonthlyNanoUSD)
	source := i18n.BillingPolicyExplicit
	if report.Policy.Inherited {
		source = i18n.BillingPolicyDefault
	}
	if report.Policy.Unlimited {
		allowance = m.text(i18n.BillingUnlimited, nil)
	}
	if report.Policy.ImplicitUnlimited {
		source = i18n.BillingPolicyAdmin
	}
	if report.AvailableNanoUSD != nil {
		available = creditAmount(*report.AvailableNanoUSD)
	}
	return m.text(
		i18n.BillingUsage,
		map[string]string{
			"allowance": allowance,
			"source":    m.text(source, nil),
			"month":     report.PeriodStart.Format("2006-01"),
			"spent":     creditAmount(report.SpentNanoUSD),
			"held":      creditAmount(report.HeldNanoUSD),
			"available": available,
			"unknown":   strconv.FormatInt(report.UnboundedUnknown, 10),
		},
	)
}

func (b *Bot) creditDefaultCommand(
	ctx context.Context,
	in incoming,
	u telegram.Update,
	m *orderMessages,
	parts []string,
) (string, error) {
	var policy credits.Policy
	endpoint := "/v1/credits/default"
	if err := b.API.Call(ctx, in.owner, http.MethodGet, endpoint, nil, &policy); err != nil {
		return "", err
	}
	if len(parts) != creditOwnerWords {
		return m.text(i18n.BillingPolicyHelp, nil), nil
	}
	change, valid := creditPolicyChange(parts[1], policy.Version, u.ID)
	if !valid || change.Unlimited || change.MonthlyNanoUSD == nil {
		return m.text(i18n.BillingPolicyHelp, nil), nil
	}
	if err := b.API.Call(ctx, in.owner, http.MethodPost, endpoint, change, &policy); err != nil {
		return "", err
	}
	return m.text(i18n.BillingSaved, nil), nil
}
func creditPolicyChange(raw string, version, updateID int64) (credits.PolicyChange, bool) {
	result := credits.PolicyChange{Version: version, OperationKey: "telegram:" + strconv.FormatInt(updateID, 10)}
	if raw == "unlimited" {
		result.Unlimited = true
		return result, true
	}
	if raw == "default" {
		return result, true
	}
	if strings.HasPrefix(raw, "-") || len(raw) > 30 {
		return result, false
	}
	parts := strings.Split(raw, ".")
	if len(parts) > creditOwnerWords || parts[0] == "" {
		return result, false
	}
	fraction := ""
	if len(parts) == creditOwnerWords {
		fraction = parts[1]
	}
	if len(fraction) > creditFractionDigits {
		return result, false
	}
	digits := parts[0] + fraction + strings.Repeat("0", creditFractionDigits-len(fraction))
	for _, char := range digits {
		if char < '0' || char > '9' {
			return result, false
		}
	}
	amount, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return result, false
	}
	result.MonthlyNanoUSD = &amount
	return result, true
}
func creditAmount(value int64) string {
	digits := strconv.FormatInt(value, 10)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	if len(digits) <= creditFractionDigits {
		digits = strings.Repeat("0", creditFractionDigits+1-len(digits)) + digits
	}
	point := len(digits) - creditFractionDigits
	whole, fraction := digits[:point], strings.TrimRight(digits[point:], "0")
	if fraction == "" {
		return sign + whole
	}
	return sign + whole + "." + fraction
}
