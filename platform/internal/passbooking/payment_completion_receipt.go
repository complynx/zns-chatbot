package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const paymentRejected = "rejected"

// PaymentCompletionReceipt contains only the exact reviewed target transition.
// The original command receipt and the current attachment must both agree.
type PaymentCompletionReceipt struct {
	Found    bool          `json:"found"`
	Decision string        `json:"decision,omitempty"`
	Attempt  string        `json:"attempt,omitempty"`
	Before   ReadAuthority `json:"before"`
	After    ReadAuthority `json:"after"`
}

func (s Service) PaymentCompletionReceipt(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	command Command,
) (PaymentCompletionReceipt, error) {
	if (command.Name != commandProofAccept && command.Name != commandProofReject) || command.Target == "" ||
		command.PaymentAttempt == "" ||
		command.TargetVersion <= 0 {
		return PaymentCompletionReceipt{}, invalid()
	}
	prepared, err := s.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return PaymentCompletionReceipt{}, err
	}
	if _, found := prepared.Replay(); !found {
		return PaymentCompletionReceipt{}, nil
	}
	target := prepared.records[command.Target]
	if target == nil || target.Version != command.TargetVersion+1 || target.CreatedAt.IsZero() {
		return PaymentCompletionReceipt{}, conflict("source_stale")
	}
	decision := paymentAccepted
	if command.Name == commandProofReject {
		decision = paymentRejected
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT COALESCE(p.decision=$4 AND p.reviewed_by=$5 AND p.reviewed_at IS NOT NULL
 AND b.assigned_at=member.assigned_at, false)
 FROM core.pass_payment_attempts p
 JOIN core.pass_payment_participants member ON member.attempt=p.id AND member.owner=$3
 JOIN core.pass_bookings b ON b.event_id=p.event_id AND b.owner=member.owner AND b.payment_attempt=p.id
 WHERE p.event_id=$1 AND p.id=$2`, command.Event, command.PaymentAttempt, command.Target, decision, actor).Scan(&valid)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !valid) {
		return PaymentCompletionReceipt{}, conflict("source_stale")
	}
	if err != nil {
		return PaymentCompletionReceipt{}, core.DatabaseOperationError(err)
	}
	before := ReadAuthority{
		Kind:           ReadPrivileged,
		Event:          command.Event,
		Owner:          command.Target,
		Version:        command.TargetVersion,
		CreatedAt:      target.CreatedAt,
		Action:         commandProofAccept,
		PaymentAttempt: command.PaymentAttempt,
	}
	after := before
	after.Version, after.PaymentAttempt = target.Version, ""
	return PaymentCompletionReceipt{
		Found:    true,
		Decision: decision,
		Attempt:  command.PaymentAttempt,
		Before:   before,
		After:    after,
	}, nil
}
