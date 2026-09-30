package delivery

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery/dbgen"
)

// LockReferences takes all lanes before any queue entries. The caller must
// already hold every owner row it will use and must roll back on an error.
func LockReferences(ctx context.Context, tx pgx.Tx, botID int64, refs []Reference) error {
	if botID <= 0 {
		return ErrQueueReference
	}
	q := dbgen.New(tx)
	seen := make(map[Reference]bool, len(refs))
	rows := make([]dbgen.ReadDeliveryEntryRow, 0, len(refs))
	for _, ref := range refs {
		if !ref.valid() {
			return ErrQueueReference
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		row, err := readQueue(ctx, q, botID, ref)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrQueueReference
		}
		if err != nil {
			return err
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Chat != rows[j].Chat {
			return rows[i].Chat < rows[j].Chat
		}
		return rows[i].LaneSequence < rows[j].LaneSequence
	})
	previous := ""
	for _, row := range rows {
		if row.Chat == previous {
			continue
		}
		if _, err := q.LockDeliveryLane(ctx, dbgen.LockDeliveryLaneParams{BotID: botID, Chat: row.Chat}); err != nil {
			return core.DatabaseOperationError(err)
		}
		previous = row.Chat
	}
	for _, row := range rows {
		if _, err := q.LockDeliveryEntry(
			ctx,
			dbgen.LockDeliveryEntryParams{
				BotID:     botID,
				OwnerKind: row.OwnerKind,
				OwnerKey:  row.OwnerKey,
				EffectKey: row.EffectKey,
			},
		); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	return nil
}

// ReadReference returns the immutable registered destination without taking locks.
// Owner authorization and attempt fencing remain the caller's responsibility.
func ReadReference(ctx context.Context, tx pgx.Tx, botID int64, ref Reference) (Entry, error) {
	if botID <= 0 || !ref.valid() {
		return Entry{}, ErrQueueReference
	}
	row, err := readQueue(ctx, dbgen.New(tx), botID, ref)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrQueueReference
	}
	if err != nil {
		return Entry{}, err
	}
	return queueEntry(row), nil
}
