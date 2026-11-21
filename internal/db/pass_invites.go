package db

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// PassInvite describes an event-specific code. Role is the role the claimant takes.
type PassInvite struct {
	Code    string     `json:"code"`
	Role    string     `json:"role"`
	EventID uuid.UUID  `json:"event_id"`
	PairID  *uuid.UUID `json:"pair_id,omitempty"`
	Revoked bool       `json:"revoked"`
}

type inviteRow struct {
	PassInvite

	issuer, registration uuid.UUID
	claimant, replaces   *uuid.UUID
}

const inviteSelect = `
SELECT i.code,i.event_id,i.pair_id,i.revoked,
 CASE WHEN r.role='leader' THEN 'follower' ELSE 'leader' END,
 r.user_id,r.id,i.claimant_user_id,i.replace_pair_id
 FROM shuttle_pass_invites i JOIN shuttle_registrations r ON r.id=i.inviter_registration_id `

func scanPassInvite(row pgx.Row) (inviteRow, error) {
	var i inviteRow
	err := row.Scan(
		&i.Code,
		&i.EventID,
		&i.PairID,
		&i.Revoked,
		&i.Role,
		&i.issuer,
		&i.registration,
		&i.claimant,
		&i.replaces,
	)
	return i, err
}

func normalizePassInvite(code string) string { return strings.ToUpper(strings.TrimSpace(code)) }

func (s *Store) passInviteTx(
	ctx context.Context,
	scope PaymentScope,
	eventID uuid.UUID,
	fn func(pgx.Tx) (PassInvite, error),
) (PassInvite, error) {
	if eventID == uuid.Nil || !paymentScopeValid(scope, "invite") {
		return PassInvite{}, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PassInvite{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := sqlc.New(tx)
	if _, err = q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return PassInvite{}, err
	}
	if err = lockPairAssignmentActor(ctx, q, scope); err != nil {
		return PassInvite{}, err
	}
	result, err := fn(tx)
	if err != nil {
		return PassInvite{}, err
	}
	return result, tx.Commit(ctx)
}

// PassInviteEvent resolves a code only inside the caller's deployment.
func (s *Store) PassInviteEvent(ctx context.Context, scope PaymentScope, code string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT i.event_id FROM shuttle_pass_invites i JOIN shuttle_events e ON e.id=i.event_id
 JOIN shuttle_users u ON u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id
 WHERE u.id=$1 AND u.zitadel_subject=$2 AND NOT u.revoked AND e.bot_id=$3 AND e.organization_id=$4
 AND i.code=$5 AND NOT i.revoked AND (i.claimant_user_id IS NULL OR i.claimant_user_id=u.id)`,
		scope.UserID, scope.Subject, scope.BotID, scope.Organization, normalizePassInvite(code)).Scan(&id)
	return id, err
}

func (s *Store) ActivePassInvite(ctx context.Context, scope PaymentScope, eventID uuid.UUID) (PassInvite, error) {
	return s.passInviteTx(ctx, scope, eventID, func(tx pgx.Tx) (PassInvite, error) {
		i, err := scanPassInvite(
			tx.QueryRow(
				ctx,
				inviteSelect+`WHERE r.user_id=$1 AND i.event_id=$2 AND i.pair_id IS NULL AND NOT i.revoked`,
				scope.UserID,
				eventID,
			),
		)
		return i.PassInvite, err
	})
}

// PendingPassClaim returns the caller's own uncompleted claim, including before registration.
func (s *Store) PendingPassClaim(ctx context.Context, scope PaymentScope, eventID uuid.UUID) (PassInvite, error) {
	i, err := scanPassInvite(s.pool.QueryRow(ctx, inviteSelect+`
 JOIN shuttle_events e ON e.id=i.event_id
 JOIN shuttle_users u ON u.id=i.claimant_user_id AND u.bot_id=e.bot_id AND u.zitadel_org=e.organization_id
 WHERE i.event_id=$1 AND u.id=$2 AND u.zitadel_subject=$3 AND NOT u.revoked
 AND e.bot_id=$4 AND e.organization_id=$5 AND i.pair_id IS NULL AND NOT i.revoked`,
		eventID, scope.UserID, scope.Subject, scope.BotID, scope.Organization))
	return i.PassInvite, err
}

func (s *Store) IssuePassInvite(
	ctx context.Context,
	scope PaymentScope,
	eventID uuid.UUID,
	operation string,
	replacePairID uuid.UUID,
) (PassInvite, error) {
	if !paymentScopeValid(scope, operation) {
		return PassInvite{}, registrations.ErrInvalid
	}
	return s.passInviteTx(ctx, scope, eventID, func(tx pgx.Tx) (PassInvite, error) {
		return issuePassInvite(ctx, tx, scope, eventID, operation, replacePairID)
	})
}

func issuePassInvite(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	eventID uuid.UUID,
	operation string,
	replacePairID uuid.UUID,
) (PassInvite, error) {
	q := sqlc.New(tx)
	i, err := scanPassInvite(tx.QueryRow(ctx, inviteSelect+
		`WHERE i.event_id=$1 AND r.user_id=$2 AND i.issue_operation=$3`, eventID, scope.UserID, operation))
	if err == nil {
		if (i.replaces == nil && replacePairID != uuid.Nil) || (i.replaces != nil && *i.replaces != replacePairID) {
			return PassInvite{}, ErrConflict
		}
		return currentIssuedInvite(ctx, q, i)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return PassInvite{}, err
	}
	event, err := registrationEventForChange(ctx, q, scope.EventScope, eventID)
	if err != nil {
		return PassInvite{}, err
	}
	r, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: eventID, UserID: scope.UserID})
	if err != nil {
		return PassInvite{}, err
	}
	if r.State != registrations.Waitlisted {
		return PassInvite{}, ErrConflict
	}
	if replacePairID == uuid.Nil {
		err = ensurePairUnheld(ctx, q, r.ID)
	} else {
		_, err = replaceablePassPair(ctx, q, r, replacePairID)
	}
	if err != nil {
		return PassInvite{}, err
	}
	if err = relinquishPassPromo(ctx, tx, eventID, scope.UserID); err != nil {
		return PassInvite{}, err
	}
	if _, err = registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, pgtype.Timestamptz{}); err != nil {
		return PassInvite{}, err
	}
	var secret [12]byte
	rand.Read(secret[:])
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret[:])
	tag, err := tx.Exec(ctx, `INSERT INTO shuttle_pass_invites
 (code,event_id,inviter_registration_id,issue_operation,replace_pair_id)
 VALUES($1,$2,$3,$4,NULLIF($5::uuid,'00000000-0000-0000-0000-000000000000'::uuid)) ON CONFLICT DO NOTHING`,
		code, eventID, r.ID, operation, replacePairID)
	if err != nil {
		return PassInvite{}, err
	}
	if tag.RowsAffected() != 1 {
		return PassInvite{}, ErrConflict
	}
	if _, err = q.AdvancePairRegistration(
		ctx,
		sqlc.AdvancePairRegistrationParams{ID: r.ID, EventID: eventID},
	); err != nil {
		return PassInvite{}, err
	}
	i, err = scanPassInvite(tx.QueryRow(ctx, inviteSelect+"WHERE i.code=$1", code))
	return i.PassInvite, err
}

func replaceablePassPair(
	ctx context.Context,
	q *sqlc.Queries,
	issuer sqlc.ShuttleRegistration,
	pairID uuid.UUID,
) (sqlc.ShuttlePair, error) {
	old, err := q.GetPair(ctx, sqlc.GetPairParams{ID: pairID, EventID: issuer.EventID})
	if err != nil {
		return old, err
	}
	if old.State != registrations.Waitlisted ||
		(old.LeaderRegistrationID != issuer.ID && old.FollowerRegistrationID != issuer.ID) {
		return old, ErrConflict
	}
	return old, nil
}

func currentIssuedInvite(ctx context.Context, q *sqlc.Queries, i inviteRow) (PassInvite, error) {
	if i.PairID != nil {
		pair, err := q.GetPair(ctx, sqlc.GetPairParams{ID: *i.PairID, EventID: i.EventID})
		if err != nil {
			return PassInvite{}, err
		}
		if pair.State == "dissolved" {
			return PassInvite{}, ErrConflict
		}
	}
	return i.PassInvite, nil
}

func (s *Store) RevokePassInvite(ctx context.Context, scope PaymentScope, eventID uuid.UUID, code string) error {
	_, err := s.passInviteTx(ctx, scope, eventID, func(tx pgx.Tx) (PassInvite, error) {
		i, err := scanPassInvite(
			tx.QueryRow(ctx, inviteSelect+"WHERE i.code=$1 AND i.event_id=$2", normalizePassInvite(code), eventID),
		)
		if err != nil {
			return PassInvite{}, err
		}
		if i.issuer != scope.UserID && (i.claimant == nil || *i.claimant != scope.UserID) {
			return PassInvite{}, pgx.ErrNoRows
		}
		if i.PairID != nil {
			return PassInvite{}, ErrConflict
		}
		_, err = tx.Exec(ctx, "UPDATE shuttle_pass_invites SET revoked=true WHERE code=$1", i.Code)
		return PassInvite{}, err
	})
	return err
}
