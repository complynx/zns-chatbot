package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) selectProofOrder(
	ctx context.Context,
	owner string,
	update int64,
	command orders.Command,
) (string, error) {
	order, err := b.API.Order(ctx, owner, command.EventID, command.OrderID)
	if err != nil {
		return b.proofFailure(ctx, owner, err)
	}
	if order.Version != command.Version || (order.State != stateUnpaid && order.State != stateCash) {
		return b.orderMessage(ctx, owner, i18n.OrderProofChanged, nil)
	}
	command.Name = stateProof
	_, err = b.DB.Exec(
		ctx,
		`INSERT INTO bot.proof_pending(owner,command,selected_update) VALUES($1,$2,$3) ON CONFLICT(owner) DO UPDATE SET command=$2,selected_update=$3 WHERE bot.proof_pending.selected_update<$3`,
		owner,
		command,
		update,
	)
	if err != nil {
		return "", err
	}
	return b.orderMessage(ctx, owner, i18n.OrderProofPrompt, map[string]string{mediaOrderChoice: order.ID})
}

func (b *Bot) proofFailure(ctx context.Context, owner string, err error) (string, error) {
	// SQL provenance wins; Telegram and document failures below stay provider controls.
	if core.IsDatabaseFailure(err) {
		return "", core.ErrDatabase
	}
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return b.orderMessage(ctx, owner, i18n.OrderProofRejected, map[string]string{orderCodeParameter: problem.Code})
	}
	if problem, ok := errors.AsType[*telegram.APIError](err); ok && problem.Code == http.StatusBadRequest {
		return b.orderMessage(ctx, owner, i18n.OrderDocumentUnavailable, nil)
	}
	if errors.Is(err, telegram.ErrInvalidDocument) {
		return b.orderMessage(ctx, owner, i18n.OrderDocumentInvalid, nil)
	}
	return "", err
}

func (b *Bot) showOrderProof(ctx context.Context, in incoming, command orders.Command) (string, error) {
	observed, err := b.queueBotDocument(
		ctx,
		in.owner,
		in.chat,
		botdelivery.Reference{
			Family:       botFamilyOrderProof,
			Event:        command.EventID,
			Object:       command.OrderID,
			Version:      command.Version,
			ProofAttempt: command.Attempt,
			Notice:       i18n.OrderProofShown,
			Continuation: botdelivery.Continuation{Kind: botDocumentKind},
		},
	)
	if err != nil {
		return b.proofFailure(ctx, in.owner, err)
	}
	if documentDelivered(observed) {
		return b.orderMessage(ctx, in.owner, i18n.OrderProofShown, nil)
	}
	return "", nil
}
