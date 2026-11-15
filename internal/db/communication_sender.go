package db

import (
	"context"

	"github.com/google/uuid"
)

func (s *Store) CommunicationSender(ctx context.Context, scope EventScope, id uuid.UUID) (string, error) {
	var name, username string
	var telegramID int64
	err := s.pool.QueryRow(ctx, `SELECT `+personNameSQL+`,`+personUsernameSQL+`,u.tg_user_id
 FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 WHERE u.id=$3 AND u.bot_id=$1 AND u.zitadel_org=$2 AND NOT u.revoked`,
		scope.BotID, scope.Organization, id).Scan(&name, &username, &telegramID)
	if err != nil {
		return "", err
	}
	return communicationName(name, username, telegramID), nil
}
