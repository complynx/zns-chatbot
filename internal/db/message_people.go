package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

// MessageRecipient finds the existing deployment account without creating one.
func (s *Store) MessageRecipient(ctx context.Context, bot int64, organization string, telegramID int64) (User, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM shuttle_users
 WHERE bot_id=$1 AND zitadel_org=$2 AND tg_user_id=$3 AND NOT revoked`, bot, organization, telegramID).Scan(&id)
	if err != nil {
		return User{}, err
	}
	return s.GetUser(ctx, id)
}

// MessagePeople resolves public/self profiles and people the caller may inspect
// through global administration, event authority or an active specialist binding.
func (s *Store) MessagePeople(
	ctx context.Context,
	scope PaymentScope,
	ids []uuid.UUID,
	telegramIDs []int64,
) (map[string]telegram.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.tg_user_id,
 coalesce(nullif(btrim(p.full_name),''),btrim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name'))),
 coalesce(u.profile->>'username','')
 FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 JOIN shuttle_users actor ON actor.id=$6 AND actor.bot_id=$1 AND actor.zitadel_org=$2
 AND actor.zitadel_subject=$7 AND NOT actor.revoked
 WHERE u.bot_id=$1 AND u.zitadel_org=$2 AND NOT u.revoked
 AND (u.visible OR u.id=actor.id OR $5 OR EXISTS(
   SELECT 1 FROM shuttle_event_authorities a JOIN shuttle_registrations r ON r.event_id=a.event_id
   JOIN shuttle_events e ON e.id=a.event_id
   WHERE a.user_id=actor.id AND a.role IN ('ambassador','administrator') AND r.user_id=u.id
   AND e.bot_id=$1 AND e.organization_id=$2
 ) OR EXISTS(
   SELECT 1 FROM shuttle_massage_bookings b
   JOIN shuttle_massage_specialist_bindings sb ON sb.event_id=b.event_id AND sb.specialist_id=b.specialist_id
   JOIN shuttle_events e ON e.id=b.event_id
   WHERE b.user_id=u.id AND b.state='booked' AND sb.active AND sb.user_id=actor.id
   AND e.bot_id=$1 AND e.organization_id=$2
 ))
 AND (u.id=ANY($3::uuid[]) OR u.tg_user_id=ANY($4::bigint[]))`, scope.BotID, scope.Organization, ids, telegramIDs,
		scope.GlobalAdmin, scope.UserID, scope.Subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	people := make(map[string]telegram.User)
	for rows.Next() {
		var id uuid.UUID
		var user telegram.User
		if err = rows.Scan(&id, &user.ID, &user.FirstName, &user.Username); err != nil {
			return nil, err
		}
		if user.ID > 0 && (user.FirstName != "" || user.Username != "") {
			people[id.String()] = user
		}
	}
	return people, rows.Err()
}
