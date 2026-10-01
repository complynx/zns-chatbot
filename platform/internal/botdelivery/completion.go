package botdelivery

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// Complete persists a known wire outcome even if caller shutdown has begun.
// The caller supplies a bounded completion context and joins this operation.
func (s Service) Complete(ctx context.Context, in CompletionRequest) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = s.complete(ctx, tx, in); err != nil {
		return err
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) complete(ctx context.Context, tx pgx.Tx, in CompletionRequest) error {
	attempt, outcome, receipt, fallback := in.Attempt, in.Outcome, in.Receipt, in.Fallback
	if attempt.BotID != s.Delivery.BotID {
		return ErrBinding
	}
	current, err := Read(ctx, tx, attempt.BotID, attempt.QueueReference(), true)
	if err != nil {
		return err
	}
	if !sameBinding(current, attempt) || current.Attempt != attempt.Attempt ||
		(current.State != delivery.Sending && current.State != delivery.Uncertain) {
		return ErrBinding
	}
	if fallback && (current.Phase != phaseEdit || outcome.Kind != delivery.Deferred) {
		return ErrBinding
	}
	outcome, deadline, err := delivery.Finish(ctx, tx, s.Delivery, current.QueueReference(), outcome)
	if err != nil {
		return err
	}
	phase, target := current.Phase, current.Target
	if fallback {
		phase, target = phaseSend, 0
	}
	if deadline.IsZero() {
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&deadline); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	if current.Reference.Family == familyPasses && current.Receipt.Pass != nil {
		receipt.Pass = current.Receipt.Pass
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.delivery_intents SET state=$4,message_id=$5,reason=$6,not_before=$7,receipt=$8,phase=$9,target_message_id=$10
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		current.BotID,
		current.Operation,
		current.Effect,
		outcome.Kind,
		outcome.MessageID,
		outcome.Reason,
		deadline,
		raw,
		phase,
		target,
	)
	return core.DatabaseOperationError(err)
}
