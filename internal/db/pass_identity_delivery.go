package db

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/registrations"
)

const (
	IdentityPrepared = "prepared"
	IdentityUnknown  = "delivery_unknown"
	IdentitySent     = "sent"
	IdentityFailed   = "failed"
	IdentityTooLarge = "too_large"
)

// IdentityDelivery records the operation binding and outcome without a reference to the archive.
type IdentityDelivery struct {
	EventID, ProposalID     uuid.UUID
	EventVersion, Recipient int64
	ExpiresAt               time.Time
	State                   string
	EventName               string
}

const readIdentityDelivery = `SELECT event_id,proposal_id,event_version,recipient,expires_at,state
 FROM shuttle_pass_identity_deliveries WHERE user_id=$1 AND operation_id=$2`

func scanIdentityDelivery(row pgx.Row) (IdentityDelivery, error) {
	var d IdentityDelivery
	err := row.Scan(&d.EventID, &d.ProposalID, &d.EventVersion, &d.Recipient, &d.ExpiresAt, &d.State)
	return d, err
}

func (s *Store) PrepareIdentityDelivery(ctx context.Context, scope PaymentScope, event uuid.UUID,
	operation string,
) (IdentityDelivery, error) {
	if event == uuid.Nil || !paymentScopeValid(scope, operation) {
		return IdentityDelivery{}, registrations.ErrInvalid
	}
	tx, q, err := s.beginPassAdminRead(ctx, scope, event, nil)
	if err != nil {
		return IdentityDelivery{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	policy, err := passIdentityPolicy(ctx, q, scope.EventScope, event)
	if err != nil {
		return IdentityDelivery{}, err
	}
	if !policy.available() {
		return IdentityDelivery{}, registrations.ErrUnavailable
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_pass_identity_deliveries
 (user_id,operation_id,event_id,event_version,recipient,proposal_id,expires_at,state)
 SELECT id,$2,$3,$4,tg_user_id,$5,clock_timestamp()+interval '5 minutes','prepared' FROM shuttle_users WHERE id=$1
 ON CONFLICT(user_id,operation_id) DO NOTHING`, scope.UserID, operation, event, policy.EventVersion, uuid.New())
	if err != nil {
		return IdentityDelivery{}, err
	}
	d, err := scanIdentityDelivery(tx.QueryRow(ctx, readIdentityDelivery, scope.UserID, operation))
	if err != nil {
		return d, err
	}
	if d.EventID != event || d.EventVersion != policy.EventVersion {
		return d, ErrConflict
	}
	d.EventName = policy.EventName
	return d, tx.Commit(ctx)
}

func (s *Store) IdentityDeliveryStatus(
	ctx context.Context,
	scope PaymentScope,
	operation string,
) (IdentityDelivery, error) {
	d, err := scanIdentityDelivery(s.pool.QueryRow(ctx, readIdentityDelivery, scope.UserID, operation))
	if err != nil {
		return d, err
	}
	tx, _, err := s.beginPassAdminRead(ctx, scope, d.EventID, nil)
	if err != nil {
		return IdentityDelivery{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	return d, nil
}

// ReserveIdentityDelivery claims the operation before the external effect. A crash or lost response leaves an
// ambiguous receipt. Replaying a confirmed operation never sends a second ZIP.
func (s *Store) ReserveIdentityDelivery(ctx context.Context, scope PaymentScope, operation string,
	proposal uuid.UUID, policy PassIdentityPolicy, refresh func(context.Context) (bool, error), tooLarge bool,
) (IdentityDelivery, bool, error) {
	tx, q, err := s.beginPassAdminRead(ctx, scope, policy.EventID, nil)
	if err != nil {
		return IdentityDelivery{}, false, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	d, err := scanIdentityDelivery(tx.QueryRow(ctx, readIdentityDelivery+" FOR UPDATE", scope.UserID, operation))
	if err != nil {
		return d, false, err
	}
	if d.ProposalID != proposal || d.EventID != policy.EventID || d.EventVersion != policy.EventVersion {
		return d, false, ErrConflict
	}
	if err = verifyPassIdentityRelease(ctx, q, scope, policy, refresh); err != nil {
		return d, false, err
	}
	if d.State != IdentityPrepared {
		return d, false, nil
	}
	d.State = IdentityUnknown
	if tooLarge {
		d.State = IdentityTooLarge
	}
	tag, err := tx.Exec(ctx, `UPDATE shuttle_pass_identity_deliveries d SET state=$3
 FROM shuttle_users u WHERE d.user_id=$1 AND d.operation_id=$2 AND d.expires_at>clock_timestamp()
 AND u.id=d.user_id AND u.tg_user_id=d.recipient`, scope.UserID, operation, d.State)
	if err != nil {
		return d, false, err
	}
	if tag.RowsAffected() != 1 {
		return d, false, ErrConflict
	}
	return d, !tooLarge, tx.Commit(ctx)
}

func (s *Store) FinishIdentityDelivery(ctx context.Context, user, proposal uuid.UUID, state string) error {
	if state != IdentitySent && state != IdentityFailed {
		return registrations.ErrInvalid
	}
	tag, err := s.pool.Exec(ctx, `UPDATE shuttle_pass_identity_deliveries SET state=$3
 WHERE user_id=$1 AND proposal_id=$2 AND state='delivery_unknown'`, user, proposal, state)
	if err == nil && tag.RowsAffected() != 1 {
		return errors.New("identity delivery receipt unavailable")
	}
	return err
}
