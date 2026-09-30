package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// BookingTransition is a committed before/after pair from a canonical receipt.
// It does not authorize a read or replace a current permission check.
type BookingTransition struct {
	Before Booking
	After  Booking
}

// AssignmentTransitions reads only earlier successful assignments of this locked
// batch. Callers retain the batch/event locks and still validate live successors.
func (b *RuntimeBatchState) AssignmentTransitions(
	ctx context.Context,
	tx pgx.Tx,
	index int,
) ([]BookingTransition, error) {
	if index < 0 || index >= len(b.plan.Items) {
		return nil, invalid()
	}
	if b.plan.Action != commandAdminAssign {
		return nil, nil
	}
	var transitions []BookingTransition
	for i, item := range b.plan.Items[:index] {
		if item.Outcome.Status != AdminBatchSucceeded {
			continue
		}
		expected := "batch-" + b.key + fmt.Sprintf("-%d", i)
		if item.Assignment.Key != expected || item.Outcome.Key != expected || item.Assignment.Event != b.plan.Event {
			return nil, conflict("source_stale")
		}
		pairs, err := assignmentTransitions(ctx, tx, b.actor, item.Assignment)
		if err != nil {
			return nil, err
		}
		transitions = append(transitions, pairs...)
	}
	return transitions, nil
}

func assignmentTransitions(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	command AdminAssignment,
) ([]BookingTransition, error) {
	raw, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	var beforeRaw, afterRaw []byte
	err = tx.QueryRow(ctx, `SELECT r.before_records,r.after_records
FROM core.pass_admin_assignments r JOIN core.pass_booking_operations o
ON o.event_id=r.event_id AND o.actor=r.actor AND o.key_hash=r.key_hash
WHERE r.event_id=$1 AND r.actor=$2 AND r.key_hash=$3 AND r.target=$4 AND o.request_hash=$5`,
		command.Event, actor, hash([]byte(command.Key)), command.Target, hash(raw)).Scan(&beforeRaw, &afterRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, conflict("source_stale")
	}
	if err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	var before, after []Booking
	if err = json.Unmarshal(beforeRaw, &before); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(afterRaw, &after); err != nil {
		return nil, err
	}
	return receiptTransitions(command.Event, before, after), nil
}

func receiptTransitions(event string, before, after []Booking) []BookingTransition {
	var result []BookingTransition
	for _, previous := range before {
		for _, next := range after {
			if previous.Event == event && next.Event == event && previous.Owner == next.Owner &&
				previous.TelegramID == next.TelegramID && previous.CreatedAt.Equal(next.CreatedAt) &&
				previous.Version > 0 && next.Version == previous.Version+1 {
				result = append(result, BookingTransition{Before: previous, After: next})
			}
		}
	}
	return result
}
