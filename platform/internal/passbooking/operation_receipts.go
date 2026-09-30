package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const operationPending = "pending"
const operationCommitted = "committed"

// OperationReceipt is a body-free projection of a canonical operation receipt.
// It is read under current domain authorization, never from a script result.
type OperationReceipt struct {
	Status          string
	Items           []OperationReceiptItem
	Transitions     []BookingTransition
	Source          json.RawMessage
	ReadAuthorities []ReadAuthority
}

type OperationReceiptItem struct {
	Index  int              `json:"index"`
	Status AdminBatchStatus `json:"status"`
	Code   string           `json:"code,omitempty"`
}

func (s Service) CommandOperationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c Command,
) (OperationReceipt, error) {
	p, err := s.PrepareInTx(ctx, tx, actor, c)
	if err != nil {
		return OperationReceipt{}, err
	}
	if c.Target != "" {
		if err = operationTarget(ctx, tx, c.Event, c.Name, c.Target); err != nil {
			return OperationReceipt{}, err
		}
	}
	_, found := p.Replay()
	result := receiptStatus(found)
	result.ReadAuthorities, err = OperationReadAuthorities(ctx, tx, actor, c.Event, c.Name, []string{c.Target})
	return result, err
}

func (s Service) AssignmentOperationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c AdminAssignment,
) (OperationReceipt, error) {
	p, err := s.PrepareAssignmentInTx(ctx, tx, actor, c)
	if err != nil {
		return OperationReceipt{}, err
	}
	if err = operationTarget(ctx, tx, c.Event, commandAdminAssign, c.Target); err != nil {
		return OperationReceipt{}, err
	}
	_, found := p.Replay()
	result := receiptStatus(found)
	if found {
		result.Transitions, err = assignmentTransitions(ctx, tx, actor, c)
	}
	if err == nil {
		result.ReadAuthorities, err = OperationReadAuthorities(
			ctx,
			tx,
			actor,
			c.Event,
			commandAdminAssign,
			[]string{c.Target},
		)
	}
	return result, err
}

func receiptStatus(found bool) OperationReceipt {
	if found {
		return OperationReceipt{Status: operationCommitted}
	}
	return OperationReceipt{Status: "not_committed"}
}

func operationTarget(ctx context.Context, tx pgx.Tx, event, action, owner string) error {
	var id int64
	err := tx.QueryRow(ctx, `SELECT telegram_id FROM core.users WHERE id=$1 FOR SHARE`, owner).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	valid, err := lockReadTarget(
		ctx,
		tx,
		ReadAuthority{Event: event, Action: action, Owner: owner, TargetTelegramID: id},
	)
	if err != nil {
		return err
	}
	if !valid {
		return forbidden()
	}
	return nil
}

// BatchOperationReceipt does not prepare or execute an item. Each successful
// outcome must have its exact canonical receipt; absent receipts fail closed.
func (s Service) BatchOperationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c RuntimeBatch,
) (OperationReceipt, error) {
	b, found, err := s.PrepareRuntimeBatchInTx(ctx, tx, actor, c)
	if err != nil {
		return OperationReceipt{}, err
	}
	if found {
		if err = b.LockRuntimeBatchInTx(ctx, tx); err != nil {
			return OperationReceipt{}, err
		}
	}
	targets := make([]string, 0, len(b.plan.Items))
	for _, item := range b.plan.Items {
		if item.Assignment.Target != "" {
			targets = append(targets, item.Assignment.Target)
			if err = operationTarget(ctx, tx, c.Event, c.Action, item.Assignment.Target); err != nil {
				return OperationReceipt{}, err
			}
		}
	}
	refs, err := OperationReadAuthorities(ctx, tx, actor, c.Event, c.Action, targets)
	if err != nil {
		return OperationReceipt{}, err
	}
	if !found {
		result := receiptStatus(false)
		result.ReadAuthorities = refs
		return result, nil
	}
	return b.operationReceipt(ctx, tx, refs)
}

func (b *RuntimeBatchState) operationReceipt(
	ctx context.Context,
	tx pgx.Tx,
	refs []ReadAuthority,
) (OperationReceipt, error) {
	result := OperationReceipt{
		Status:          "complete",
		Source:          b.Source,
		Items:           make([]OperationReceiptItem, 0, len(b.plan.Items)),
		ReadAuthorities: refs,
	}
	for i, item := range b.plan.Items {
		switch item.Outcome.Status {
		case AdminBatchNotAttempted, AdminBatchInterrupted:
			result.Status = operationPending
		case AdminBatchSucceeded, AdminBatchRejected:
		default:
			return OperationReceipt{}, conflict("source_stale")
		}
		if item.Outcome.Status == AdminBatchSucceeded {
			transitions, receiptErr := b.operationItemReceipt(ctx, tx, i, item)
			if receiptErr != nil {
				return OperationReceipt{}, receiptErr
			}
			result.Transitions = append(result.Transitions, transitions...)
		}
		result.Items = append(
			result.Items,
			OperationReceiptItem{Index: i, Status: item.Outcome.Status, Code: item.Outcome.Code},
		)
	}
	return result, nil
}

func (b *RuntimeBatchState) operationItemReceipt(
	ctx context.Context,
	tx pgx.Tx,
	index int,
	item RuntimeBatchItem,
) ([]BookingTransition, error) {
	expected := "batch-" + b.key + fmt.Sprintf("-%d", index)
	a := item.Assignment
	if a.Key != expected || item.Outcome.Key != expected || a.Event != b.plan.Event {
		return nil, conflict("source_stale")
	}
	if b.plan.Action == commandAdminAssign {
		return assignmentTransitions(ctx, tx, b.actor, a)
	}
	c := Command{
		Name:          b.plan.Action,
		Event:         a.Event,
		Key:           a.Key,
		Version:       a.Version,
		Target:        a.Target,
		TargetVersion: a.TargetVersion,
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_booking_operations WHERE actor=$1 AND event_id=$2 AND key_hash=$3 AND request_hash=$4)`, b.actor, c.Event, hash([]byte(c.Key)), hash(raw)).
		Scan(&valid)
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	if !valid {
		return nil, conflict("source_stale")
	}
	return nil, nil
}
