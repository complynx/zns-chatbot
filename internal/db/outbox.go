package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const (
	deliveryPending   = "pending"
	deliveryRetryable = "retryable"
	deliveryBlocked   = "delivery_blocked"
	deliveryDelivered = "delivered"
	deliveryUnknown   = "delivery_unknown"
	deliveryFailed    = "delivery_failed"
)

func delivery(d sqlc.ShuttleOutbox) Delivery {
	return Delivery{d.ID, d.TaskID, d.DedupKey, d.ChunkSeq, d.Payload, d.State, uuid.UUID(d.AttemptID.Bytes), d.Receipt}
}
func (s *Store) ListOutbox(ctx context.Context, id uuid.UUID) ([]Delivery, error) {
	rows, e := s.q.ListOutbox(ctx, id)
	if e != nil {
		return nil, e
	}
	out := make([]Delivery, len(rows))
	for i, r := range rows {
		out[i] = delivery(r)
	}
	return out, nil
}

// BlockPendingDelivery retains an unsent receipt after local access revocation.
func (s *Store) BlockPendingDelivery(ctx context.Context, id uuid.UUID) error {
	return s.q.BlockPendingDelivery(ctx, id)
}

// ReserveDelivery persists uncertainty before the caller performs network I/O.
func (s *Store) ReserveDelivery(ctx context.Context, id uuid.UUID) (Delivery, error) {
	d, e := s.q.ReserveDelivery(
		ctx,
		sqlc.ReserveDeliveryParams{ID: id, AttemptID: pgtype.UUID{Bytes: uuid.New(), Valid: true}},
	)
	return delivery(d), e
}

func (s *Store) FinishDelivery(
	ctx context.Context,
	id, attempt uuid.UUID,
	state string,
	receipt json.RawMessage,
) (DeliveryReceipt, error) {
	if state != deliveryDelivered && state != deliveryUnknown && state != "retryable" && state != deliveryFailed {
		return DeliveryReceipt{}, fmt.Errorf("invalid delivery outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	params := sqlc.FinishDeliveryParams{
		ID: id, AttemptID: pgtype.UUID{Bytes: attempt, Valid: true}, State: state, Receipt: receipt,
	}
	if state == deliveryDelivered {
		return s.finishConfirmedDelivery(ctx, params)
	}
	d, e := s.q.FinishDelivery(ctx, params)
	return DeliveryReceipt{Delivery: delivery(d)}, e
}

func (s *Store) finishConfirmedDelivery(
	ctx context.Context,
	params sqlc.FinishDeliveryParams,
) (DeliveryReceipt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	id, err := q.DeliveryTaskID(ctx, params.ID)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	// Save locks the task before writing outbox rows. Keep that order here too.
	if err = q.LockResponseTask(ctx, id); err != nil {
		return DeliveryReceipt{}, err
	}
	d, err := q.FinishDelivery(ctx, params)
	if err != nil {
		return DeliveryReceipt{}, err
	}
	result := DeliveryReceipt{Delivery: delivery(d)}
	seconds, err := q.CompleteFinalResponse(ctx, sqlc.CompleteFinalResponseParams{TaskID: id, ChunkSeq: d.ChunkSeq})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return DeliveryReceipt{}, err
	}
	if err == nil {
		result.ResponseElapsedSeconds = &seconds
	}
	if err = tx.Commit(ctx); err != nil {
		return DeliveryReceipt{}, err
	}
	return result, nil
}

// RecoverUnknownDeliveries runs before workers start in the single-process MVP.
// A crash during send cannot establish whether Telegram accepted the message.
func (s *Store) RecoverUnknownDeliveries(ctx context.Context) (int64, error) {
	return s.q.RecoverUnknownDeliveries(ctx)
}

// BlockDeliverySuffix makes undeliverable later chunks durable after a terminal send.
func (s *Store) BlockDeliverySuffix(ctx context.Context, taskID uuid.UUID, afterChunk int32) error {
	return s.q.BlockDeliverySuffix(ctx, sqlc.BlockDeliverySuffixParams{TaskID: taskID, ChunkSeq: afterChunk})
}
