package passbooking

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// WitnessBatch reads only the existing canonical batch for lock planning. It
// cannot ground, create or execute an operation from an opaque witness.
func (s Service) WitnessBatch(ctx context.Context, actor string, w OperationWitness) (*RuntimeBatchState, error) {
	if !w.Valid(actor) || w.Family != OperationBatch {
		return nil, forbidden()
	}
	b := &RuntimeBatchState{actor: actor, key: hash([]byte(w.Key))}
	var digest string
	var plan []byte
	err := s.DB.QueryRow(ctx, `SELECT request_hash,plan,source_derivation FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2`, actor, b.key).
		Scan(&digest, &plan, &b.Source)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	if err = json.Unmarshal(plan, &b.plan); err != nil {
		return nil, err
	}
	if digest != w.Digest || !witnessBatchMatches(w, b) {
		return nil, conflict("source_stale")
	}
	return b, nil
}

func witnessBatchMatches(w OperationWitness, b *RuntimeBatchState) bool {
	if b.plan.Event != w.Event || b.plan.Action != w.Action || len(b.plan.Items) != len(w.Recipients) {
		return false
	}
	for i, item := range b.plan.Items {
		if item.TelegramID != w.Recipients[i] {
			return false
		}
	}
	return true
}

// WitnessOperationReceipt permits only read-only, context-free receipt status.
// A caller must never use this witness to resume or reconstruct a command.
func (s Service) WitnessOperationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	w OperationWitness,
	batch *RuntimeBatchState,
) (OperationReceipt, error) {
	if !w.Valid(actor) {
		return OperationReceipt{}, forbidden()
	}
	if _, err := readEvent(ctx, tx, w.Event); err != nil {
		return OperationReceipt{}, err
	}
	if _, err := authorize(ctx, tx, actor, w.Action, w.Event); err != nil {
		return OperationReceipt{}, err
	}
	if w.QueueInvitation {
		if _, err := authorize(ctx, tx, actor, commandAdminAssign, w.Event); err != nil {
			return OperationReceipt{}, err
		}
	}
	if w.Family == OperationBatch {
		return s.witnessBatchReceipt(ctx, tx, actor, w, batch)
	}
	refs, err := OperationReadAuthorities(ctx, tx, actor, w.Event, w.Action, []string{w.Target})
	if err != nil {
		return OperationReceipt{}, err
	}
	if w.QueueInvitation {
		refs = append(refs, ReadAuthority{Kind: ReadCapability, Event: w.Event, Action: commandAdminAssign})
	}
	var digest string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM core.pass_booking_operations WHERE actor=$1 AND event_id=$2 AND key_hash=$3`, actor, w.Event, hash([]byte(w.Key))).
		Scan(&digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return OperationReceipt{Status: "not_committed", ReadAuthorities: refs}, nil
	}
	if err != nil {
		return OperationReceipt{}, core.DatabaseOperationContextError(ctx, err)
	}
	if digest != w.Digest {
		return OperationReceipt{}, conflict("source_stale")
	}
	return OperationReceipt{Status: operationCommitted, ReadAuthorities: refs}, nil
}

func (s Service) witnessBatchReceipt(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	w OperationWitness,
	b *RuntimeBatchState,
) (OperationReceipt, error) {
	// Without a persisted plan there is no canonical target grounding to reuse.
	// Retired admissions cannot create that plan from their digest.
	if b == nil {
		return OperationReceipt{}, forbidden()
	}
	if err := b.LockRuntimeBatchInTx(ctx, tx); err != nil {
		return OperationReceipt{}, err
	}
	if !witnessBatchMatches(w, b) {
		return OperationReceipt{}, conflict("source_stale")
	}
	var digest string
	if err := tx.QueryRow(ctx, `SELECT request_hash FROM core.pass_admin_batches WHERE actor=$1 AND key_hash=$2`, actor, b.key).
		Scan(&digest); err != nil {
		return OperationReceipt{}, core.DatabaseOperationContextError(ctx, err)
	}
	if digest != w.Digest {
		return OperationReceipt{}, conflict("source_stale")
	}
	targets := make([]string, 0, len(b.plan.Items))
	for _, item := range b.plan.Items {
		targets = append(targets, item.Assignment.Target)
	}
	refs, err := OperationReadAuthorities(ctx, tx, actor, w.Event, w.Action, targets)
	if err != nil {
		return OperationReceipt{}, err
	}
	return b.operationReceipt(ctx, tx, refs)
}
