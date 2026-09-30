package botdelivery

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// ApplyReceipt commits local projections, child notices and the continuation
// marker together. It never calls transport or fresh identity under its locks.
func (s Service) ApplyReceipt(ctx context.Context, in ReceiptRequest) error {
	observed := in.Observed
	if observed.BotID != s.Delivery.BotID || observed.State != delivery.Succeeded || observed.MessageID <= 0 {
		return ErrBinding
	}
	current, err := Read(ctx, s.DB, observed.BotID, observed.QueueReference(), false)
	if err != nil {
		return err
	}
	if !sameReceipt(current, observed) {
		return ErrBinding
	}
	if current.ContinuationDone {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	denied := false
	if err = s.lockSource(ctx, tx, current); err != nil {
		if !sourceDenied(err) {
			return err
		}
		denied = true
	}
	key := fmt.Sprintf("%d:%s:%s", current.BotID, current.Operation, current.Effect)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,81081))", key); err != nil {
		return core.DatabaseOperationError(err)
	}
	current, err = Read(ctx, tx, observed.BotID, observed.QueueReference(), true)
	if err != nil {
		return err
	}
	if !sameReceipt(current, observed) {
		return ErrBinding
	}
	if current.ContinuationDone {
		return core.DatabaseOperationError(tx.Commit(ctx))
	}
	if !denied {
		if err = s.projectReceipt(ctx, tx, current); err != nil {
			return err
		}
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.delivery_intents SET continuation_done=true WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND state='sent' AND attempt=$4 AND message_id=$5`,
		current.BotID,
		current.Operation,
		current.Effect,
		current.Attempt,
		current.MessageID,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
func sameReceipt(a, b Intent) bool {
	return sameBinding(a, b) && a.State == delivery.Succeeded && a.Attempt == b.Attempt && a.MessageID == b.MessageID &&
		reflect.DeepEqual(a.Receipt, b.Receipt)
}
func sourceDenied(err error) bool {
	if errors.Is(err, ErrStale) {
		return true
	}
	p, ok := errors.AsType[*core.ProblemError](err)
	return ok &&
		(p.Code == "history_stale" || p.Code == codePassSourceStale || p.Code == "source_revoked" || p.Status == 403)
}
func (s Service) projectReceipt(ctx context.Context, tx pgx.Tx, i Intent) error {
	if i.State != delivery.Succeeded || i.MessageID <= 0 {
		return ErrBinding
	}
	r := i.Receipt
	var err error
	switch r.Kind {
	case "":
		return nil
	case "workflow_card":
		_, err = tx.Exec(ctx, `INSERT INTO bot.messages(owner,chat_id,message_id,view_hash) VALUES($1,$2,$3,$4)
 ON CONFLICT(owner) DO UPDATE SET chat_id=$2,message_id=$3,view_hash=$4`, i.Owner, i.Chat, i.MessageID, r.ViewHash)
	case "order_card":
		_, err = tx.Exec(
			ctx,
			`INSERT INTO bot.order_cards(owner,card_key,chat_id,message_id,view_hash,visible) VALUES($1,$2,$3,$4,$5,$6)
 ON CONFLICT(owner,card_key) DO UPDATE SET chat_id=$3,message_id=$4,view_hash=$5,visible=$6`,
			i.Owner,
			r.Key,
			i.Chat,
			i.MessageID,
			r.ViewHash,
			!r.Retired,
		)
	case "pass_card", "massage_card":
		if r.Kind == "pass_card" {
			_, err = tx.Exec(
				ctx,
				"UPDATE bot.pass_views SET message_id=$2,view_hash=$3 WHERE owner=$1",
				i.Owner,
				i.MessageID,
				r.ViewHash,
			)
			if err == nil {
				_, err = tx.Exec(
					ctx,
					"DELETE FROM bot.pass_buttons WHERE owner=$1 AND revision<=$2 AND NOT(token=ANY($3))",
					i.Owner,
					r.Revision,
					r.Tokens,
				)
			}
		} else {
			_, err = tx.Exec(
				ctx,
				"UPDATE bot.massage_views SET message_id=$2,view_hash=$3 WHERE owner=$1",
				i.Owner,
				i.MessageID,
				r.ViewHash,
			)
			if err == nil {
				_, err = tx.Exec(
					ctx,
					"DELETE FROM bot.massage_buttons WHERE owner=$1 AND revision<=$2 AND NOT(token=ANY($3))",
					i.Owner,
					r.Revision,
					r.Tokens,
				)
			}
		}
		return core.DatabaseOperationError(err)
	case familyPassRedaction:
		_, err = tx.Exec(ctx, "UPDATE bot.pass_views SET message_id=$2 WHERE owner=$1", i.Owner, i.MessageID)
	default:
		return s.projectResultReceipt(ctx, tx, i)
	}
	return core.DatabaseOperationError(err)
}
func (s Service) projectResultReceipt(ctx context.Context, tx pgx.Tx, i Intent) error {
	switch i.Receipt.Kind {
	case "admin_prompt":
		return s.AdminMessages.RegisterPromptInTx(ctx, tx, i.Owner, i.Receipt.ID, i.Chat, i.MessageID)
	case "admin_expiry":
		return s.AdminMessages.CompleteInputExpiryInTx(ctx, tx, i.Owner, i.Chat, i.Receipt.ID)
	default:
		return s.projectDocumentReceipt(ctx, tx, i)
	}
}
