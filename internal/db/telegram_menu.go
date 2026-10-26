package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// TelegramMenuAvailability is a navigation hint. Each command still checks its
// current authorization when it runs.
type TelegramMenuAvailability struct {
	Registrations, Register, Orders, Massage, Pairs, Payments bool
}

// MenuAvailability reads one snapshot of the named owner's native user routes.
// Retained registration, pair and massage history stays discoverable when the
// corresponding module no longer accepts new work.
func (s *Store) MenuAvailability(ctx context.Context, scope ProfileScope) (TelegramMenuAvailability, error) {
	var result TelegramMenuAvailability
	if scope.BotID <= 0 || scope.Organization == "" || scope.Subject == "" || scope.UserID == uuid.Nil {
		return result, pgx.ErrNoRows
	}
	err := s.pool.QueryRow(ctx, `WITH actor AS (
 SELECT u.id FROM shuttle_users u JOIN shuttle_user_profiles profile ON profile.user_id=u.id
 WHERE u.id=$1 AND u.bot_id=$2 AND u.zitadel_org=$3 AND u.zitadel_subject=$4
 AND NOT u.revoked AND profile.first_name<>'' AND profile.last_name<>''
), events AS (
 SELECT e.id,e.modules, ('registrations'=ANY(e.modules) AND e.ends_at>statement_timestamp()
 AND (rules.document->>'closes_at')::timestamptz>statement_timestamp()) AS registration_open
 FROM shuttle_events e LEFT JOIN shuttle_event_registration_rules rules ON rules.event_id=e.id
 WHERE e.bot_id=$2 AND e.organization_id=$3
), own AS (
 SELECT r.id,r.event_id,r.state,p.id AS pass_id FROM shuttle_registrations r JOIN events e ON e.id=r.event_id
 JOIN actor ON actor.id=r.user_id LEFT JOIN shuttle_passes p ON p.registration_id=r.id
)
SELECT
 EXISTS(SELECT 1 FROM own),
 EXISTS(SELECT 1 FROM events e WHERE e.registration_open
 AND NOT EXISTS(SELECT 1 FROM own r WHERE r.event_id=e.id)),
 EXISTS(SELECT 1 FROM own r JOIN events e ON e.id=r.event_id
 WHERE r.state='assigned' AND r.pass_id IS NOT NULL AND 'orders'=ANY(e.modules)),
 EXISTS(SELECT 1 FROM events e WHERE
 EXISTS(SELECT 1 FROM shuttle_massage_bookings b WHERE b.event_id=e.id AND b.user_id=actor.id)
 OR EXISTS(SELECT 1 FROM shuttle_massage_preparations pending WHERE pending.event_id=e.id
 AND pending.user_id=actor.id AND pending.consumed_at IS NULL AND pending.expires_at>statement_timestamp())
 OR ('massage'=ANY(e.modules) AND EXISTS(SELECT 1 FROM own r
 WHERE r.event_id=e.id AND r.state='assigned' AND r.pass_id IS NOT NULL))),
 (EXISTS(SELECT 1 FROM own r JOIN events e ON e.id=r.event_id
 WHERE r.state='waitlisted' AND e.registration_open)
 OR EXISTS(SELECT 1 FROM shuttle_pairs pair JOIN own r
 ON r.id=pair.leader_registration_id OR r.id=pair.follower_registration_id
 WHERE pair.state IN ('waitlisted','assigned'))
 OR EXISTS(SELECT 1 FROM shuttle_pair_invitations invitation JOIN events e ON e.id=invitation.event_id
 JOIN shuttle_registrations inviter ON inviter.id=invitation.inviter_registration_id
 WHERE invitation.target_user_id=actor.id OR inviter.user_id=actor.id)),
 EXISTS(SELECT 1 FROM own r JOIN events e ON e.id=r.event_id
 WHERE r.state='assigned' AND r.pass_id IS NOT NULL AND 'registrations'=ANY(e.modules))
FROM actor`, scope.UserID, scope.BotID, scope.Organization, scope.Subject).
		Scan(&result.Registrations, &result.Register, &result.Orders, &result.Massage, &result.Pairs, &result.Payments)
	return result, err
}

// MenuRecipients returns a bounded page of linked users, including registered,
// unnamed and revoked owners. Unnamed and revoked recipients permit clearing a
// previous menu; MenuAvailability grants them no entries. Advance afterID to the
// last returned user's ID.
func (s *Store) MenuRecipients(ctx context.Context, botID int64, organization string,
	afterID uuid.UUID, limit int,
) ([]User, error) {
	if botID <= 0 || organization == "" || limit < 0 || limit > 100 {
		return nil, ErrInvalidProfile
	}
	if limit == 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.bot_id,u.tg_user_id,u.profile,
u.zitadel_subject,u.zitadel_org,u.revoked
FROM shuttle_users u
WHERE u.bot_id=$1 AND u.zitadel_org=$2 AND u.zitadel_subject<>''
AND u.id>$3
ORDER BY u.id LIMIT $4`, botID, organization, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]User, 0, limit)
	for rows.Next() {
		var recipient User
		if err = rows.Scan(&recipient.ID, &recipient.BotID, &recipient.TelegramUserID, &recipient.Profile,
			&recipient.Subject, &recipient.Organization, &recipient.Revoked); err != nil {
			return nil, err
		}
		result = append(result, recipient)
	}
	return result, rows.Err()
}

// MenuRecipientByTelegram resolves one linked owner for a synchronous refresh.
// Revoked owners remain addressable so an existing menu can be cleared.
func (s *Store) MenuRecipientByTelegram(ctx context.Context, botID int64, organization string,
	telegramID int64,
) (User, error) {
	var recipient User
	if botID <= 0 || organization == "" || telegramID <= 0 {
		return recipient, pgx.ErrNoRows
	}
	err := s.pool.QueryRow(ctx, `SELECT id,bot_id,tg_user_id,profile,zitadel_subject,zitadel_org,revoked
FROM shuttle_users WHERE bot_id=$1 AND zitadel_org=$2 AND tg_user_id=$3 AND zitadel_subject<>''`,
		botID, organization, telegramID).
		Scan(&recipient.ID, &recipient.BotID, &recipient.TelegramUserID, &recipient.Profile,
			&recipient.Subject, &recipient.Organization, &recipient.Revoked)
	return recipient, err
}
