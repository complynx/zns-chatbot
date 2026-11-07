package db

import "context"

// HasTelegramProfileName is an intake routing hint, not an authorization check.
// Dispatch still verifies the current identity and profile before using them.
func (s *Store) HasTelegramProfileName(
	ctx context.Context,
	botID, telegramID int64,
	organization string,
) (bool, error) {
	var named bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(
SELECT 1 FROM shuttle_users u JOIN shuttle_user_profiles p ON p.user_id=u.id
WHERE u.bot_id=$1 AND u.tg_user_id=$2 AND u.zitadel_org=$3
AND u.zitadel_subject<>'' AND NOT u.revoked AND p.first_name<>'' AND p.last_name<>'')`,
		botID, telegramID, organization).Scan(&named)
	return named, err
}
