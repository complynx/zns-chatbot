package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/shuttle/internal/db/sqlc"
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
) (Delivery, error) {
	if state != "delivered" && state != "delivery_unknown" && state != "retryable" && state != "delivery_failed" {
		return Delivery{}, fmt.Errorf("invalid delivery outcome")
	}
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	d, e := s.q.FinishDelivery(
		ctx,
		sqlc.FinishDeliveryParams{
			ID:        id,
			AttemptID: pgtype.UUID{Bytes: attempt, Valid: true},
			State:     state,
			Receipt:   receipt,
		},
	)
	return delivery(d), e
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
