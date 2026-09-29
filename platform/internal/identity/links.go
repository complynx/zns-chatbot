package identity

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Links resolves pre-provisioned bindings, never mutable names or email claims.
type Links struct {
	DB     *pgxpool.Pool
	Issuer string
	BotID  int64
}

type User struct{ Owner, Subject string }

func (l Links) Telegram(ctx context.Context, telegramID int64) (User, error) {
	var user User
	err := l.DB.QueryRow(ctx, `SELECT z.owner,z.subject FROM core.zitadel_identities z
 JOIN core.telegram_identities t ON t.owner=z.owner JOIN core.users u ON u.id=z.owner
 WHERE z.issuer=$1 AND z.active AND t.bot_id=$2 AND t.telegram_id=$3 AND u.telegram_id=$3`,
		l.Issuer, l.BotID, telegramID).Scan(&user.Owner, &user.Subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrZitadelIdentity
	}
	return user, err
}

func (l Links) Subject(ctx context.Context, subject string) (string, error) {
	var owner string
	err := l.DB.QueryRow(ctx, `SELECT owner FROM core.zitadel_identities WHERE issuer=$1 AND subject=$2 AND active`, l.Issuer, subject).
		Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrZitadelIdentity
	}
	return owner, err
}

// Bind is an operator/importer provisioning operation, not an HTTP or model tool.
// Existing bindings must match exactly; it never merges or silently reassigns users.
func (l Links) Bind(ctx context.Context, owner string, telegramID int64, subject string) error {
	if owner == "" || l.Issuer == "" || subject == "" {
		return ErrZitadelIdentity
	}
	tx, err := l.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existingTelegram int64
	if err = tx.QueryRow(ctx, `SELECT telegram_id FROM core.users WHERE id=$1 FOR UPDATE`, owner).
		Scan(&existingTelegram); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrZitadelIdentity
		}
		return err
	}
	if existingTelegram != telegramID {
		return ErrZitadelIdentity
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.zitadel_identities(owner,issuer,subject) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		owner,
		l.Issuer,
		subject,
	)
	if err != nil {
		return err
	}
	var matches bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.zitadel_identities WHERE owner=$1 AND issuer=$2 AND subject=$3 AND active)`, owner, l.Issuer, subject).
		Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return ErrZitadelIdentity
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.telegram_identities(bot_id,telegram_id,owner) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		l.BotID,
		telegramID,
		owner,
	)
	if err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.telegram_identities WHERE bot_id=$1 AND telegram_id=$2 AND owner=$3)`, l.BotID, telegramID, owner).
		Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return ErrZitadelIdentity
	}
	return tx.Commit(ctx)
}
