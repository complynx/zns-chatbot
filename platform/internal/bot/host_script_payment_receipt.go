package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (p botScriptAuthority) PassPaymentReceipt(
	ctx context.Context,
	owner string,
	command passbooking.Command,
	source readsource.Derivation,
) (passbooking.PaymentCompletionReceipt, error) {
	return p.bot.Host.PassPaymentReceipt(ctx, owner, command, source)
}
