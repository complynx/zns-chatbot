package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type PassAssignmentNotice struct {
	EventID, UserID   uuid.UUID
	Event, Zone, Role string
	Pass              registrations.Pass
}

// ReadyPassAssignmentNotices returns deliverable notices for frozen owner passes.
func (s *Store) ReadyPassAssignmentNotices(
	ctx context.Context,
	scope EventScope,
	after uuid.UUID,
) ([]NotificationJob, error) {
	rows, err := s.pool.Query(ctx, `
 SELECT n.pass_id,n.version
 FROM shuttle_pass_assignment_notices n JOIN shuttle_passes p ON p.id=n.pass_id
   JOIN shuttle_registrations r ON r.id=p.registration_id
 JOIN shuttle_events e ON e.id=r.event_id WHERE e.bot_id=$1 AND e.organization_id=$2 AND n.pass_id>$3
 AND n.state IN ('pending','retryable') AND n.next_attempt_at<=clock_timestamp() ORDER BY n.pass_id
   LIMIT 100
`, scope.BotID, scope.Organization, after)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (NotificationJob, error) {
		var j NotificationJob
		scanErr := row.Scan(&j.ID, &j.Version)
		return j, scanErr
	})
}

func (s *Store) PassAssignmentNotice(
	ctx context.Context,
	scope EventScope,
	job NotificationJob,
) (PassAssignmentNotice, error) {
	var n PassAssignmentNotice
	var snapshot []byte
	err := s.pool.QueryRow(ctx, `
 SELECT p.id,e.id,r.user_id,e.name,e.timezone,r.role,p.price_minor,p.currency,p.currency_scale,
   p.payment_due_at,d.document
 FROM shuttle_pass_assignment_notices n JOIN shuttle_passes p ON p.id=n.pass_id
   JOIN shuttle_registrations r ON r.id=p.registration_id
 LEFT JOIN shuttle_pass_deposit_policy d ON d.pass_id=p.id
 JOIN shuttle_events e ON e.id=r.event_id WHERE n.pass_id=$1 AND e.bot_id=$2 AND e.organization_id=$3
   AND n.version=$4
 AND n.state IN ('pending','retryable')
`, job.ID, scope.BotID, scope.Organization, job.Version).Scan(
		&n.Pass.ID, &n.EventID, &n.UserID, &n.Event, &n.Zone, &n.Role,
		&n.Pass.PriceMinor, &n.Pass.Currency, &n.Pass.CurrencyScale, &n.Pass.PaymentDueAt, &snapshot,
	)
	if err == nil && snapshot != nil {
		err = json.Unmarshal(snapshot, &n.Pass.Deposit)
	}
	return n, err
}

// ReservePassAssignmentNotice locks the event through the final registration eligibility check.
func (s *Store) ReservePassAssignmentNotice(
	ctx context.Context,
	scope PaymentScope,
	job NotificationJob,
	eventID uuid.UUID,
) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Take the event and owner locks before reading the registration. An in-flight
	// return can change its state while the reservation waits for the event.
	_, _, err = lockPaymentEvent(ctx, s.q.WithTx(tx), scope, eventID, false)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, payments.ErrDenied) || errors.Is(err, payments.ErrUnavailable) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `
 UPDATE shuttle_pass_assignment_notices n SET state='sending' WHERE n.pass_id=$1 AND n.version=$2
   AND n.state IN ('pending','retryable') AND n.next_attempt_at<=clock_timestamp()
 AND EXISTS(SELECT 1 FROM shuttle_passes p JOIN shuttle_registrations r ON r.id=p.registration_id
 WHERE p.id=n.pass_id AND r.state='assigned' AND r.user_id=$3 AND r.event_id=$4)
`, job.ID, job.Version, scope.UserID, eventID)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	err = tx.Commit(ctx)
	return err == nil, err
}

func (s *Store) FinishPassAssignmentNotice(
	ctx context.Context,
	job NotificationJob,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	_, err := s.pool.Exec(ctx, `
 UPDATE shuttle_pass_assignment_notices
   SET state=$3,receipt=$4,version=version+1,
   next_attempt_at=clock_timestamp()+($5::bigint*interval '1 millisecond')
 WHERE pass_id=$1 AND version=$2 AND ((state='sending'
   AND $3::text IN ('delivered','retryable','delivery_unknown','delivery_failed'))
   OR (state IN('pending','retryable') AND $3='delivery_blocked'))
`, job.ID, job.Version, state, receipt, delay.Milliseconds())
	return err
}

func (s *Store) RecoverPassAssignmentNotices(ctx context.Context, scope EventScope) error {
	_, err := s.pool.Exec(ctx, `
 UPDATE shuttle_pass_assignment_notices n SET state='delivery_unknown',version=n.version+1
 FROM shuttle_passes p JOIN shuttle_registrations r ON r.id=p.registration_id
   JOIN shuttle_events e ON e.id=r.event_id
 WHERE n.state='sending' AND n.pass_id=p.id AND e.bot_id=$1 AND e.organization_id=$2
`, scope.BotID, scope.Organization)
	return err
}
