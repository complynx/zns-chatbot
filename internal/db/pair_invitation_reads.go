package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type PairInvitationView struct {
	Invitation    registrations.Invitation `json:"invitation"`
	EventName     string                   `json:"event_name"`
	Timezone      string                   `json:"timezone"`
	StartsAt      time.Time                `json:"starts_at"`
	EndsAt        time.Time                `json:"ends_at"`
	InviterUserID uuid.UUID                `json:"inviter_user_id"`
	InviterName   string                   `json:"inviter_name"`
	RequiredRole  string                   `json:"required_role"`
}

type PairInvitationList struct {
	Direction string
	EventID   uuid.UUID
	State     string
	After     uuid.UUID
	Limit     int32
}

func (s *Store) PairInvitations(
	ctx context.Context,
	scope PaymentScope,
	args PairInvitationList,
) ([]PairInvitationView, error) {
	if !paymentScopeValid(scope, "read") || args.Limit < 1 || args.Limit > pairReadPageLimit ||
		(args.Direction != "incoming" && args.Direction != "outgoing") || !validInvitationState(args.State) {
		return nil, registrations.ErrInvalid
	}
	rows, err := s.q.ListPairInvitations(
		ctx,
		sqlc.ListPairInvitationsParams{
			UserID:         scope.UserID,
			Subject:        scope.Subject,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Direction:      args.Direction,
			EventID:        args.EventID,
			FilterState:    args.State,
			AfterID:        args.After,
			PageLimit:      args.Limit,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]PairInvitationView, 0, len(rows))
	for _, row := range rows {
		role := registrations.Leader
		if row.InviterRole == registrations.Leader {
			role = registrations.Follower
		}
		result = append(
			result,
			PairInvitationView{
				Invitation:    pairInvitationRecord(row.ShuttlePairInvitation),
				EventName:     row.EventName,
				Timezone:      row.Timezone,
				StartsAt:      row.StartsAt.Time.UTC(),
				EndsAt:        row.EndsAt.Time.UTC(),
				InviterUserID: row.InviterUserID,
				InviterName:   row.InviterName,
				RequiredRole:  role,
			},
		)
	}
	return result, nil
}

func validInvitationState(state string) bool {
	switch state {
	case "", pairPending, pairAccepted, "declined", "revoked", pairExpired:
		return true
	default:
		return false
	}
}

func lockInvitationActor(ctx context.Context, q *sqlc.Queries, scope PaymentScope, eventID uuid.UUID) error {
	if _, err := q.LockRegistrationEvent(
		ctx,
		sqlc.LockRegistrationEventParams{ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization},
	); err != nil {
		return err
	}
	actor, err := q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return err
	}
	if actor.ID != scope.UserID {
		return ErrPairDenied
	}
	return nil
}

// PairInvitationOperation returns a historical receipt, not the invitation's current state.
func (s *Store) PairInvitationOperation(
	ctx context.Context,
	scope PaymentScope,
	operation, kind string,
) (PairResult, error) {
	if !paymentScopeValid(scope, operation) ||
		(kind != pairInvite && kind != pairAccept && kind != pairDecline && kind != pairRevoke) {
		return PairResult{}, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	row, err := q.GetPairOperation(
		ctx,
		sqlc.GetPairOperationParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    operation,
		},
	)
	if err != nil {
		return PairResult{}, err
	}
	if row.Kind != kind {
		return PairResult{}, pgx.ErrNoRows
	}
	var result PairResult
	if err = json.Unmarshal(row.Result, &result); err != nil {
		return result, err
	}
	if err = lockInvitationActor(ctx, q, scope, result.Invitation.EventID); err != nil {
		return PairResult{}, err
	}
	if kind == pairAccept || kind == pairDecline {
		if result.Invitation.TargetUserID != scope.UserID {
			return PairResult{}, ErrPairDenied
		}
	} else {
		inviter, readErr := q.GetRegistrationByID(
			ctx,
			sqlc.GetRegistrationByIDParams{
				ID:      result.Invitation.InviterRegistrationID,
				EventID: result.Invitation.EventID,
			},
		)
		if readErr != nil {
			return PairResult{}, readErr
		}
		if inviter.UserID != scope.UserID {
			return PairResult{}, ErrPairDenied
		}
	}
	return result, nil
}

type PairInvitationContext struct {
	EventID         uuid.UUID `json:"event_id"`
	EventName       string    `json:"event_name"`
	Timezone        string    `json:"timezone"`
	RegistrationID  uuid.UUID `json:"registration_id"`
	ExpectedVersion int64     `json:"expected_version"`
	Role            string    `json:"role"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// PairInvitationContext explains the caller's hold policy before an invitation request.
// It does not reserve capacity or guarantee that a future invitation is eligible.
func (s *Store) PairInvitationContext(
	ctx context.Context,
	scope PaymentScope,
	eventID uuid.UUID,
) (PairInvitationContext, error) {
	if !paymentScopeValid(scope, "read") || eventID == uuid.Nil {
		return PairInvitationContext{}, registrations.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PairInvitationContext{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = lockInvitationActor(ctx, q, scope, eventID); err != nil {
		return PairInvitationContext{}, err
	}
	event, err := registrationEventForChange(ctx, q, scope.EventScope, eventID)
	if err != nil {
		return PairInvitationContext{}, err
	}
	entry, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: eventID, UserID: scope.UserID})
	if err != nil {
		return PairInvitationContext{}, err
	}
	if entry.State != registrations.Waitlisted {
		return PairInvitationContext{}, ErrConflict
	}
	return PairInvitationContext{
		EventID:         eventID,
		EventName:       event.Document.Name,
		Timezone:        event.Document.Timezone,
		RegistrationID:  entry.ID,
		ExpectedVersion: entry.Version,
		Role:            entry.Role,
		ExpiresAt:       event.Document.Registration.ClosesAt.UTC(),
	}, nil
}
