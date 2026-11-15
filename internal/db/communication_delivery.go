package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type CommunicationDelivery struct {
	ID                       uuid.UUID  `json:"id"`
	CommunicationID          uuid.UUID  `json:"communication_id"`
	RecipientID              uuid.UUID  `json:"recipient_id"`
	Kind                     string     `json:"kind"`
	State                    string     `json:"state"`
	Version                  int64      `json:"version"`
	AttemptID                uuid.UUID  `json:"-"`
	Text                     string     `json:"-"`
	PersonalizationRunID     string     `json:"-"`
	PersonalizationStartedAt *time.Time `json:"-"`
	PersonalizationPolls     int        `json:"-"`
}

func (s *Store) ReadyCommunications(ctx context.Context, scope EventScope, after uuid.UUID) ([]NotificationJob, error) {
	rows, err := s.pool.Query(ctx, `
SELECT d.id,d.version FROM shuttle_communication_deliveries d
 JOIN shuttle_communications c ON c.id=d.communication_id WHERE c.bot_id=$1 AND
c.organization_id=$2
 AND d.id>$3 AND d.state IN ('pending','retryable','sending') AND
d.next_attempt_at<=clock_timestamp()
 ORDER BY d.id LIMIT $4
`, scope.BotID, scope.Organization, after, NotificationRepairPageSize)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []NotificationJob{}
	for rows.Next() {
		var job NotificationJob
		if err = rows.Scan(&job.ID, &job.Version); err != nil {
			return nil, err
		}
		result = append(result, job)
	}
	return result, rows.Err()
}

func (s *Store) CommunicationDelivery(
	ctx context.Context,
	scope EventScope,
	id uuid.UUID,
) (CommunicationDelivery, Communication, error) {
	var d CommunicationDelivery
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return d, Communication{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
SELECT d.id,d.communication_id,d.recipient_id,d.kind,d.state,d.version,

coalesce(d.attempt_id,'00000000-0000-0000-0000-000000000000'::uuid),coalesce(d.rendered_text,''),
coalesce(d.personalization_run_id,''),d.personalization_started_at,d.personalization_polls
 FROM shuttle_communication_deliveries d JOIN shuttle_communications c ON
c.id=d.communication_id
 WHERE d.id=$1 AND c.bot_id=$2 AND c.organization_id=$3
`, id, scope.BotID, scope.Organization).
		Scan(&d.ID, &d.CommunicationID, &d.RecipientID, &d.Kind, &d.State, &d.Version, &d.AttemptID, &d.Text,
			&d.PersonalizationRunID, &d.PersonalizationStartedAt, &d.PersonalizationPolls)
	if err != nil {
		return d, Communication{}, err
	}
	c, err := readCommunication(ctx, tx, scope, d.CommunicationID, false)
	return d, c, err
}

// ReserveCommunication rechecks subscriptions and relationship grants immediately before sending.
func (s *Store) ReserveCommunication(
	ctx context.Context,
	scope PaymentScope,
	d CommunicationDelivery,
	c Communication,
	targetAdmin bool,
) (CommunicationDelivery, error) {
	return s.checkCommunicationDelivery(ctx, scope, d, c, targetAdmin, true)
}

func (s *Store) CheckCommunicationDelivery(
	ctx context.Context,
	scope PaymentScope,
	d CommunicationDelivery,
	c Communication,
	targetAdmin bool,
) error {
	_, err := s.checkCommunicationDelivery(ctx, scope, d, c, targetAdmin, false)
	return err
}

func (s *Store) checkCommunicationDelivery(
	ctx context.Context,
	scope PaymentScope,
	d CommunicationDelivery,
	c Communication,
	targetAdmin, reserve bool,
) (CommunicationDelivery, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return d, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockCommunicationActor(ctx, tx, scope, c.Arguments.EventID); err != nil {
		return d, err
	}
	c, err = readCommunication(ctx, tx, scope.EventScope, c.ID, true)
	if err != nil {
		return d, err
	}
	if scope.UserID != c.AuthorID {
		return d, ErrPeopleDenied
	}
	if d.Kind == "confirmation" {
		if d.RecipientID != scope.UserID || c.State != "prepared" || !time.Now().Before(c.ExpiresAt) {
			return d, ErrPeopleDenied
		}
	} else if err = checkCommunicationMessage(ctx, tx, scope, d, c, targetAdmin); err != nil {
		return d, err
	}
	if !reserve {
		return d, tx.Commit(ctx)
	}
	d.AttemptID = uuid.New()
	err = tx.QueryRow(ctx, `
UPDATE shuttle_communication_deliveries SET state='sending',version=version+1,
 attempt_id=$3,next_attempt_at=clock_timestamp()+interval '5 minutes'
 WHERE id=$1 AND version=$2 AND state IN ('pending','retryable') RETURNING version
`, d.ID, d.Version, d.AttemptID).Scan(&d.Version)
	if err != nil {
		return d, err
	}
	return d, tx.Commit(ctx)
}

func (s *Store) FinishCommunication(
	ctx context.Context,
	d CommunicationDelivery,
	state string,
	receipt json.RawMessage,
	delay time.Duration,
) error {
	if len(receipt) == 0 {
		receipt = json.RawMessage(`{}`)
	}
	tag, err := s.pool.Exec(ctx, `
UPDATE shuttle_communication_deliveries SET state=$3,receipt=$4,version=version+1,
 personalization_input=NULL,rendered_text=CASE WHEN $3='retryable' THEN rendered_text ELSE NULL END,
 next_attempt_at=clock_timestamp()+$5*interval '1 millisecond' WHERE id=$1 AND
attempt_id=$2
AND state='sending'
`, d.ID, d.AttemptID, state, receipt, delay.Milliseconds())
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}

func (s *Store) BlockCommunication(ctx context.Context, d CommunicationDelivery) error {
	_, err := s.pool.Exec(
		ctx,
		`
UPDATE shuttle_communication_deliveries SET
state='delivery_blocked',version=version+1,personalization_input=NULL,rendered_text=NULL WHERE
id=$1 AND version=$2 AND state IN ('pending','retryable')
`,
		d.ID,
		d.Version,
	)
	return err
}

func (s *Store) FailCommunication(ctx context.Context, d CommunicationDelivery) error {
	_, err := s.pool.Exec(ctx, `UPDATE shuttle_communication_deliveries
 SET state='delivery_failed',version=version+1,personalization_input=NULL,rendered_text=NULL
 WHERE id=$1 AND version=$2 AND state IN ('pending','retryable')`, d.ID, d.Version)
	return err
}

func (s *Store) RecoverCommunications(ctx context.Context, scope EventScope) error {
	_, err := s.pool.Exec(ctx, `
UPDATE shuttle_communication_deliveries d SET
state='delivery_unknown',version=version+1,personalization_input=NULL,rendered_text=NULL
 FROM shuttle_communications c WHERE c.id=d.communication_id AND c.bot_id=$1 AND
c.organization_id=$2 AND d.state='sending'
`, scope.BotID, scope.Organization)
	return err
}

func checkCommunicationMessage(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	d CommunicationDelivery,
	c Communication,
	targetAdmin bool,
) error {
	var err error

	if c.State != "queued" {
		return ErrPeopleDenied
	}
	if err = communicationAccess(
		ctx,
		tx,
		scope,
		c.Arguments.EventID,
		d.RecipientID,
		c.Category == communicationPersonal,
		targetAdmin,
	); err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`
SELECT pg_advisory_xact_lock(hashtextextended($1,0))
`,
		"broadcast-subscription:"+d.RecipientID.String(),
	); err != nil {
		return err
	}
	allowed, e := communicationSubscribed(ctx, tx, d.RecipientID, c.Arguments.EventID, c.Category)
	if e != nil {
		return e
	}
	if !allowed {
		return ErrPeopleDenied
	}
	return nil
}
