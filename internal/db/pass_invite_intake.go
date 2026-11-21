package db

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// PassInviteInput resolves a bare argument to an event first, then an invitation code.
type PassInviteInput struct {
	Organization, Event, Code, Role string
}

// intakeUser locks the selected event before either participant account. The
// inbox entry, user identity, and any promo or invitation claim commit together.
func (s *Store) intakeUser(ctx context.Context, tx pgx.Tx, r IntakeRequest) (sqlc.ShuttleUser, error) {
	selected, err := passIntakeEvent(ctx, tx, r)
	if err != nil {
		return sqlc.ShuttleUser{}, err
	}
	var inviteEvent uuid.UUID
	if selected == nil && r.PassInvite != nil {
		inviteEvent, err = passIntakeInviteEvent(ctx, tx, r)
		if err != nil {
			return sqlc.ShuttleUser{}, err
		}
	}
	u, err := sqlc.New(tx).UpsertUser(ctx, sqlc.UpsertUserParams{
		ID: uuid.New(), BotID: r.BotID, TgUserID: r.TelegramUserID, Profile: r.Profile,
	})
	if err != nil {
		return u, err
	}
	if u.Revoked {
		return u, &UserRevokedError{PublicReason: u.BanPublicReason}
	}
	switch {
	case inviteEvent != uuid.Nil:
		err = savePassIntakeInvite(ctx, tx, r, u, inviteEvent)
	case selected != nil:
		scope := EventScope{BotID: r.BotID, Organization: r.PassTicket.Organization}
		err = s.savePassIntakeChoice(ctx, tx, selected, u, scope)
	}
	return u, err
}

func (i PassInviteInput) code() string {
	if i.Code != "" {
		return i.Code
	}
	return i.Event
}

// Lock the event before the Telegram user, in the same order as the allocator.
func passIntakeInviteEvent(ctx context.Context, tx pgx.Tx, r IntakeRequest) (uuid.UUID, error) {
	i := r.PassInvite
	if r.TelegramUserID <= 0 || r.ChatID != r.TelegramUserID || i.Organization == "" || i.code() == "" {
		return uuid.Nil, nil
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT e.id FROM shuttle_pass_invites i JOIN shuttle_events e ON e.id=i.event_id
 WHERE e.bot_id=$1 AND e.organization_id=$2 AND i.code=$3 AND NOT i.revoked
 AND (CASE WHEN $4 THEN NOT EXISTS(SELECT 1 FROM shuttle_events other WHERE other.bot_id=e.bot_id
  AND other.organization_id=e.organization_id AND (other.slug=$5 OR other.id::text=$5))
 ELSE ($5='' OR e.slug=$5 OR e.id::text=$5) END)`,
		r.BotID, i.Organization, normalizePassInvite(i.code()), i.Code == "", i.Event).Scan(&id)
	if err == nil {
		_, err = sqlc.New(tx).LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
			ID: id, BotID: r.BotID, OrganizationID: i.Organization,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, nil
	}
	return id, err
}

func savePassIntakeInvite(
	ctx context.Context,
	tx pgx.Tx,
	r IntakeRequest,
	owner sqlc.ShuttleUser,
	eventID uuid.UUID,
) error {
	i := r.PassInvite
	if owner.ZitadelOrg != "" && owner.ZitadelOrg != i.Organization {
		return nil
	}
	scope := PaymentScope{BotID: r.BotID, Organization: i.Organization, UserID: owner.ID}
	// Invalid invitations still enter the inbox for a normal reply. Roll back
	// the claim and any related promo release together on a rejected attempt.
	err := pgx.BeginFunc(ctx, tx, func(savepoint pgx.Tx) error {
		_, err := claimPassInviteTx(ctx, savepoint, scope, eventID, i.code(), reserveAtIntake, []string{i.Role})
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, ErrConflict) || errors.Is(err, registrations.ErrUnavailable) {
		return nil
	}
	return err
}
