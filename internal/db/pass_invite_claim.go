package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// ClaimPassInvite records the first eligible claimant even when they still need
// to complete their own registration. Both writes run under the event lock.
func (s *Store) ClaimPassInvite(
	ctx context.Context,
	scope PaymentScope,
	eventID uuid.UUID,
	code string,
	requestedRole ...string,
) (PassInvite, error) {
	return s.passInviteTx(ctx, scope, eventID, func(tx pgx.Tx) (PassInvite, error) {
		return claimPassInviteTx(ctx, tx, scope, eventID, code, claimAndComplete, requestedRole)
	})
}

// ReservedPassInvite checks the claim captured at Telegram intake. Registration
// completes the pair after saving the supplied details.
func (s *Store) ReservedPassInvite(
	ctx context.Context,
	scope PaymentScope,
	eventID uuid.UUID,
	code string,
	requestedRole ...string,
) (PassInvite, error) {
	return s.passInviteTx(ctx, scope, eventID, func(tx pgx.Tx) (PassInvite, error) {
		return claimPassInviteTx(ctx, tx, scope, eventID, code, readReserved, requestedRole)
	})
}

type inviteClaimAction int

const (
	claimAndComplete inviteClaimAction = iota
	reserveAtIntake
	readReserved
)

func claimPassInviteTx(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	eventID uuid.UUID,
	code string,
	action inviteClaimAction,
	requestedRole []string,
) (PassInvite, error) {
	q := sqlc.New(tx)
	i, err := scanPassInvite(
		tx.QueryRow(ctx, inviteSelect+"WHERE i.code=$1 AND i.event_id=$2", normalizePassInvite(code), eventID),
	)
	if err != nil {
		return PassInvite{}, err
	}
	if len(requestedRole) > 1 || (len(requestedRole) == 1 && requestedRole[0] != "" && requestedRole[0] != i.Role) {
		return PassInvite{}, ErrConflict
	}
	if i.Revoked || i.issuer == scope.UserID || (i.claimant != nil && *i.claimant != scope.UserID) {
		return PassInvite{}, ErrConflict
	}
	if action == readReserved && i.claimant == nil {
		return PassInvite{}, ErrConflict
	}
	if i.PairID != nil {
		return currentIssuedInvite(ctx, q, i)
	}
	if err = pairUser(ctx, q, scope, i.issuer); err != nil {
		return PassInvite{}, err
	}
	if _, err = registrationEventForChange(ctx, q, scope.EventScope, eventID); err != nil {
		return PassInvite{}, err
	}
	issuer, err := q.GetRegistrationByID(ctx, sqlc.GetRegistrationByIDParams{ID: i.registration, EventID: eventID})
	if err != nil {
		return PassInvite{}, err
	}
	if issuer.State != registrations.Waitlisted {
		return PassInvite{}, ErrConflict
	}
	target, err := reservePassClaimant(ctx, tx, i, scope.UserID)
	if err != nil {
		return PassInvite{}, err
	}
	if target.ID == uuid.Nil || action != claimAndComplete {
		return i.PassInvite, nil
	}
	result, err := completePassInvite(ctx, tx, scope, i, target)
	if errors.Is(err, registrationdetails.ErrRequired) {
		return i.PassInvite, nil
	}
	return result, err
}

func reservePassClaimant(
	ctx context.Context,
	tx pgx.Tx,
	i inviteRow,
	userID uuid.UUID,
) (sqlc.ShuttleRegistration, error) {
	q := sqlc.New(tx)
	target, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: i.EventID, UserID: userID})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return target, err
	}
	if target.ID != uuid.Nil && (target.State != registrations.Waitlisted || target.Role != i.Role) {
		return target, ErrConflict
	}
	if i.claimant != nil {
		return target, nil
	}
	if target.ID != uuid.Nil {
		if err = ensurePairUnheld(ctx, q, target.ID); err != nil {
			return target, err
		}
	}
	if err = relinquishPassPromo(ctx, tx, i.EventID, userID); err != nil {
		return target, err
	}
	tag, err := tx.Exec(ctx, `UPDATE shuttle_pass_invites SET claimant_user_id=$2 WHERE code=$1 AND NOT EXISTS
 (SELECT 1 FROM shuttle_pass_invites WHERE event_id=$3 AND claimant_user_id=$2 AND pair_id IS NULL AND NOT revoked)`,
		i.Code, userID, i.EventID)
	if err == nil && tag.RowsAffected() != 1 {
		err = ErrConflict
	}
	return target, err
}

func finishPassInviteRegistration(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	r sqlc.ShuttleRegistration,
	operation string,
	selection *RegistrationEntry,
) (sqlc.ShuttleRegistration, error) {
	i, err := scanPassInvite(tx.QueryRow(ctx, inviteSelect+`
 WHERE i.event_id=$1 AND i.claimant_user_id=$2 AND i.pair_id IS NULL AND NOT i.revoked`, r.EventID, r.UserID))
	issue := selection != nil && selection.IssueInvite
	if selection != nil && selection.ClaimInviteCode != "" {
		expected := normalizePassInvite(selection.ClaimInviteCode)
		if errors.Is(err, pgx.ErrNoRows) {
			return r, checkCompletedPassClaim(ctx, tx, r, expected)
		}
		if err == nil && expected != i.Code {
			return r, ErrConflict
		}
	}
	switch {
	case err == nil:
		if issue {
			return r, ErrConflict
		}
		_, err = completePassInvite(ctx, tx, scope, i, r)
	case !errors.Is(err, pgx.ErrNoRows):
		return r, err
	case issue:
		_, err = issuePassInvite(ctx, tx, scope, r.EventID, operation, uuid.Nil)
	default:
		return r, nil
	}
	if err != nil {
		return r, err
	}
	return sqlc.New(tx).GetRegistrationByID(ctx, sqlc.GetRegistrationByIDParams{ID: r.ID, EventID: r.EventID})
}

func checkCompletedPassClaim(ctx context.Context, tx pgx.Tx, r sqlc.ShuttleRegistration, expected string) error {
	var paired bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_pass_invites i
 JOIN shuttle_active_pair_members m ON m.pair_id=i.pair_id
 WHERE i.code=$1 AND i.event_id=$2 AND i.claimant_user_id=$3 AND m.registration_id=$4 AND NOT i.revoked)`,
		expected, r.EventID, r.UserID, r.ID).Scan(&paired)
	if err == nil && !paired {
		return ErrConflict
	}
	return err
}

func completePassInvite(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	i inviteRow,
	target sqlc.ShuttleRegistration,
) (PassInvite, error) {
	q := sqlc.New(tx)
	if target.Role != i.Role || target.State != registrations.Waitlisted {
		return PassInvite{}, ErrConflict
	}
	if err := pairUser(ctx, q, scope, i.issuer); err != nil {
		return PassInvite{}, err
	}
	event, err := registrationEventForChange(ctx, q, scope.EventScope, i.EventID)
	if err != nil {
		return PassInvite{}, err
	}
	if err = requireRegistrationDetails(ctx, q, event, target.UserID); err != nil {
		return PassInvite{}, err
	}
	issuer, err := q.GetRegistrationByID(ctx, sqlc.GetRegistrationByIDParams{ID: i.registration, EventID: i.EventID})
	if err != nil {
		return PassInvite{}, err
	}
	if issuer.State != registrations.Waitlisted || issuer.Role == target.Role {
		return PassInvite{}, ErrConflict
	}
	if err = ensureNoActivePair(ctx, q, target.ID); err != nil {
		return PassInvite{}, err
	}
	if i.replaces != nil {
		old, readErr := replaceablePassPair(ctx, q, issuer, *i.replaces)
		if readErr != nil {
			return PassInvite{}, readErr
		}
		if _, err = dissolvePairMembership(ctx, q, old); err != nil {
			return PassInvite{}, err
		}
	} else if err = ensureNoActivePair(ctx, q, issuer.ID); err != nil {
		return PassInvite{}, err
	}
	now, err := registrationMutationTime(ctx, q, event.Document.Registration.ClosesAt, pgtype.Timestamptz{})
	if err != nil {
		return PassInvite{}, err
	}
	pair, err := insertWaitlistedPair(ctx, q, issuer, target, now)
	if err != nil {
		return PassInvite{}, err
	}
	if _, err = q.AdvancePairRegistration(
		ctx,
		sqlc.AdvancePairRegistrationParams{ID: issuer.ID, EventID: i.EventID},
	); err != nil {
		return PassInvite{}, err
	}
	_, err = tx.Exec(ctx, "UPDATE shuttle_pass_invites SET pair_id=$2,revoked=false WHERE code=$1", i.Code, pair.ID)
	i.PairID = &pair.ID
	return i.PassInvite, err
}
