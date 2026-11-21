package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) ClaimExternalApproval(
	ctx context.Context, operation, user, attempt uuid.UUID, expiry time.Time, presentationHash []byte,
) (int, bool, error) {
	var nextPart int
	err := s.pool.QueryRow(ctx, `WITH expired AS (
		SELECT operation_id FROM shuttle_external_approval_messages
		WHERE expires_at<=clock_timestamp() AND operation_id<>$1
		ORDER BY expires_at LIMIT 32 FOR UPDATE SKIP LOCKED
	), pruned AS (
		DELETE FROM shuttle_external_approval_messages m USING expired e WHERE m.operation_id=e.operation_id
	)
	INSERT INTO shuttle_external_approval_messages AS m(
		operation_id,user_id,delivery_attempt,expires_at,lease_until,presentation_hash)
		SELECT $1,$2,$3,$4,clock_timestamp()+interval '1 minute',$5 WHERE $4>clock_timestamp()
		ON CONFLICT (operation_id) DO UPDATE SET delivery_attempt=$3,lease_until=EXCLUDED.lease_until,
			next_part=CASE WHEN m.presentation_hash=$5 THEN m.next_part ELSE 0 END,
			presentation_hash=$5
		WHERE m.user_id=$2 AND m.expires_at=$4 AND m.message_id IS NULL AND m.lease_until<=clock_timestamp()
		RETURNING next_part`, operation, user, attempt, expiry, presentationHash).Scan(&nextPart)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return nextPart, err == nil, err
}

func (s *Store) SaveExternalApprovalPart(
	ctx context.Context, operation, attempt uuid.UUID, index int, keyboardMessage int64,
) error {
	tag, err := s.pool.Exec(ctx, `UPDATE shuttle_external_approval_messages
		SET next_part=next_part+1,message_id=NULLIF($4::bigint,0)
		WHERE operation_id=$1 AND delivery_attempt=$2 AND next_part=$3 AND message_id IS NULL`,
		operation, attempt, index, keyboardMessage)
	if err == nil && tag.RowsAffected() != 1 {
		return pgx.ErrNoRows
	}
	return err
}

func (s *Store) ReleaseExternalApproval(ctx context.Context, operation, attempt uuid.UUID, delay time.Duration) error {
	_, err := s.pool.Exec(ctx, `UPDATE shuttle_external_approval_messages
		SET lease_until=clock_timestamp()+make_interval(secs=>$3)
		WHERE operation_id=$1 AND delivery_attempt=$2 AND message_id IS NULL`, operation, attempt, delay.Seconds())
	return err
}

func (s *Store) ExternalApprovalDelivered(ctx context.Context, operation, user uuid.UUID) (bool, error) {
	var delivered bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_external_approval_messages
		WHERE operation_id=$1 AND user_id=$2 AND message_id IS NOT NULL)`, operation, user).Scan(&delivered)
	return delivered, err
}

func (s *Store) CanAnswerExternalApproval(ctx context.Context, operation, user uuid.UUID, message int64) (bool, error) {
	var allowed bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_external_approval_messages
		WHERE operation_id=$1 AND user_id=$2 AND expires_at>clock_timestamp()
		AND message_id=$3)`,
		operation, user, message).Scan(&allowed)
	return allowed, err
}
