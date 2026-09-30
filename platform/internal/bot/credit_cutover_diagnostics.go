package bot

import (
	"context"
	"errors"
	"log/slog"

	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// A closed diagnostic contains no configuration values or external error text.
// LogValue preserves the operator's migration instruction through the safe logger.
type creditCutoverError uint8

const (
	errCreditCutoverEnforcement creditCutoverError = iota
	errCreditCutoverLegacyLimit
)

func (d creditCutoverError) Error() string {
	if d == errCreditCutoverLegacyLimit {
		return "remove the legacy assistant_daily_limit override; configure monetary allowance separately before restarting"
	}
	return "enable credits enforcement on every application instance before restarting after credit cutover"
}

func (d creditCutoverError) LogValue() slog.Value {
	code, field := "credit_cutover_enforcement", "credits_enforce"
	if d == errCreditCutoverLegacyLimit {
		code, field = "credit_cutover_legacy_limit", "assistant_daily_limit"
	}
	return slog.GroupValue(slog.String("code", code), slog.String("field", field), slog.String("reason", d.Error()))
}

func (b *Bot) creditCutoverConfiguration(active bool) error {
	if !active {
		return nil
	}
	if !b.CreditsEnforce {
		return errCreditCutoverEnforcement
	}
	if b.AssistantDailyLimit != 0 && b.AssistantDailyLimit != config.DefaultAssistantDailyLimit {
		return errCreditCutoverLegacyLimit
	}
	return nil
}

// Check before intake as well as inside budget admission: cutover can activate
// while a process is already polling. Pending inbox rows remain recoverable.
func (b *Bot) validateCreditCutover(ctx context.Context) error {
	var active bool
	if err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credits.cutover WHERE singleton)`).
		Scan(&active); err != nil {
		return core.DatabaseOperationError(err)
	}
	return b.creditCutoverConfiguration(active)
}

// Report the closed diagnostic before application shutdown joins it with other
// errors. Ordinary shutdown and arbitrary errors retain their existing behavior.
func (b *Bot) creditCutoverRunResult(ctx context.Context, err error) error {
	if ctx.Err() != nil && !core.IsDatabaseFailure(err) {
		err = nil
	}
	if diagnostic, ok := errors.AsType[creditCutoverError](err); ok {
		b.logger().ErrorContext(ctx, "bot credit cutover configuration invalid", "error", diagnostic)
	}
	return err
}
