package identityprovision

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (s Service) bind(ctx context.Context, tx pgx.Tx, telegramID int64, b Binding, c Creation, ready bool) error {
	if !ready {
		commands := []struct {
			query string
			args  []any
		}{
			{`INSERT INTO core.users(id,telegram_id,name,can_book,language,first_name,last_name,print_name)
 VALUES($1,$2,$3,true,$4,$5,$6,$3) ON CONFLICT DO NOTHING`, []any{b.Owner, telegramID, strings.TrimSpace(c.FirstName + " " + c.LastName), c.Language, c.FirstName, c.LastName}},
			{`INSERT INTO core.pass_profiles(owner) VALUES($1) ON CONFLICT DO NOTHING`, []any{b.Owner}},
			{
				`INSERT INTO core.zitadel_identities(owner,issuer,subject) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
				[]any{b.Owner, s.Issuer, b.Subject},
			},
			{
				`INSERT INTO core.telegram_identities(bot_id,telegram_id,owner) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
				[]any{s.BotID, telegramID, b.Owner},
			},
		}
		for _, command := range commands {
			if _, err := tx.Exec(ctx, command.query, command.args...); err != nil {
				return core.DatabaseOperationError(err)
			}
		}
	}
	var matches bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users u JOIN core.zitadel_identities z ON z.owner=u.id
 JOIN core.telegram_identities t ON t.owner=u.id JOIN core.pass_profiles p ON p.owner=u.id
 WHERE u.id=$1 AND u.telegram_id=$2 AND z.issuer=$3 AND z.subject=$4 AND z.active AND t.bot_id=$5 AND t.telegram_id=$2)`,
		b.Owner, telegramID, s.Issuer, b.Subject, s.BotID).Scan(&matches)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !matches {
		return ErrConflict
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.identity_provisioning SET ready=true WHERE issuer=$1 AND bot_id=$2 AND telegram_id=$3`,
		s.Issuer,
		s.BotID,
		telegramID,
	)
	return core.DatabaseOperationError(err)
}
