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

// MessagePeople resolves the same public basic identity as ResolvePerson for
// exact references. Directory/card authority does not restrict these fields.
func (s *Store) MessagePeople(
	ctx context.Context,
	scope PaymentScope,
	ids []uuid.UUID,
	telegramIDs []int64,
) (map[string]telegram.User, error) {
	rows, err := s.pool.Query(ctx, `SELECT u.id,u.tg_user_id,`+personNameSQL+`,`+personUsernameSQL+`
 FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 JOIN shuttle_users actor ON actor.id=$5 AND actor.bot_id=$1 AND actor.zitadel_org=$2
 AND actor.zitadel_subject=$6 AND NOT actor.revoked
 WHERE u.bot_id=$1 AND u.zitadel_org=$2
 AND (u.id=ANY($3::uuid[]) OR u.tg_user_id=ANY($4::bigint[]))`, scope.BotID, scope.Organization, ids, telegramIDs,
		scope.UserID, scope.Subject)
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
		if user.ID > 0 {
			user.FirstName = PersonPreferredName(user.FirstName, user.Username, user.ID)
			people[id.String()] = user
		}
	}
	return people, rows.Err()
}
