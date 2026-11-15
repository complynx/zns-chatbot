package db

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// AdmitCommunicationPersonalization bounds upstream runs across this deployment.
// The durable start time also bounds recovery after a crashed setup attempt.
func (s *Store) AdmitCommunicationPersonalization(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
	limit int,
) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended($1,0))`,
		fmt.Sprintf("communication-generation:%d:%s", scope.BotID, scope.Organization),
	)
	if err != nil {
		return false, err
	}
	var started *time.Time
	err = tx.QueryRow(ctx, `SELECT d.personalization_started_at FROM shuttle_communication_deliveries d
 JOIN shuttle_communications c ON c.id=d.communication_id
 WHERE d.id=$1 AND c.bot_id=$2 AND c.organization_id=$3 AND d.kind='message'
 AND d.state IN ('pending','retryable') FOR UPDATE OF d`, id, scope.BotID, scope.Organization).Scan(&started)
	if err != nil {
		return false, err
	}
	if started != nil {
		return true, tx.Commit(ctx)
	}
	var active int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM shuttle_communication_deliveries d
 JOIN shuttle_communications c ON c.id=d.communication_id
 WHERE c.bot_id=$1 AND c.organization_id=$2 AND d.personalization_started_at IS NOT NULL
 AND d.rendered_text IS NULL AND d.state IN ('pending','retryable')`, scope.BotID, scope.Organization).Scan(&active)
	if err != nil {
		return false, err
	}
	if active >= limit {
		return false, tx.Commit(ctx)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE shuttle_communication_deliveries SET personalization_started_at=clock_timestamp(),
 personalization_polls=0 WHERE id=$1`,
		id,
	)
	if err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (s *Store) ScheduleCommunicationPersonalization(
	ctx context.Context,
	d CommunicationDelivery,
	delay time.Duration,
) error {
	tag, err := s.pool.Exec(ctx, `UPDATE shuttle_communication_deliveries SET
 personalization_polls=personalization_polls+1,next_attempt_at=clock_timestamp()+$4*interval '1 millisecond'
 WHERE id=$1 AND version=$2 AND personalization_polls=$3 AND state IN ('pending','retryable')`,
		d.ID, d.Version, d.PersonalizationPolls, delay.Milliseconds())
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}
