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
// needsAdmin reports otherwise-hidden candidates, never unrelated identifiers.
// Relationship grants and bounded names are shared with directory reads.
func (s *Store) MessagePeople(
	ctx context.Context,
	scope PaymentScope,
	ids []uuid.UUID,
	telegramIDs []int64,
) (map[string]telegram.User, bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.tg_user_id,`+personNameSQL+`,`+personUsernameSQL+`,
 (u.visible OR u.id=actor.id OR $5 OR EXISTS(
   SELECT 1 FROM shuttle_event_authorities a JOIN shuttle_registrations r ON r.event_id=a.event_id
   JOIN shuttle_events e ON e.id=a.event_id
   WHERE a.user_id=actor.id AND `+peopleAuthorityRolesSQL+` AND r.user_id=u.id
   AND e.bot_id=$1 AND e.organization_id=$2
 ) OR EXISTS(`+peopleMassageClientSQL+` AND sb.user_id=actor.id
   AND EXISTS(SELECT 1 FROM shuttle_events e WHERE e.id=b.event_id AND e.bot_id=$1 AND e.organization_id=$2)
 )) AS readable
 FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 JOIN shuttle_users actor ON actor.id=$6 AND actor.bot_id=$1 AND actor.zitadel_org=$2
 AND actor.zitadel_subject=$7 AND NOT actor.revoked
 WHERE u.bot_id=$1 AND u.zitadel_org=$2 AND NOT u.revoked
 AND (u.id=ANY($3::uuid[]) OR u.tg_user_id=ANY($4::bigint[]))`, scope.BotID, scope.Organization, ids, telegramIDs,
		scope.GlobalAdmin, scope.UserID, scope.Subject)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	people := make(map[string]telegram.User)
	needsAdmin := false
	for rows.Next() {
		var id uuid.UUID
		var user telegram.User
		var readable bool
		if err = rows.Scan(&id, &user.ID, &user.FirstName, &user.Username, &readable); err != nil {
			return nil, false, err
		}
		if user.ID > 0 && (user.FirstName != "" || user.Username != "") {
			if readable {
				people[id.String()] = user
			} else {
				needsAdmin = true
			}
		}
	}
	return people, needsAdmin, rows.Err()
}
