// Package broadcastprofile records explicit current fields without inventing absent source values.
package broadcastprofile

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// Sender contains only fields written by an accepted account metadata refresh.
type Sender struct {
	ID                                       int64
	Username, FirstName, LastName, PrintName string
}

// Telegram records the accepted, ordered Telegram sender refresh in its transaction.
func Telegram(ctx context.Context, tx pgx.Tx, owner string, sender Sender) error {
	fields := map[string]any{"user_id": sender.ID, "username": sender.Username,
		"first_name": sender.FirstName, "last_name": sender.LastName, "print_name": sender.PrintName}
	return write(ctx, tx, owner, fields)
}

// Language records an explicit preference write, never a default from a read.
func Language(ctx context.Context, tx pgx.Tx, owner, value string) error {
	return write(ctx, tx, owner, map[string]any{"language_code": value})
}

// PassField records only the field actually changed by an accepted profile command.
func PassField(ctx context.Context, tx pgx.Tx, owner, field, value string) error {
	switch field {
	case "role", "legal_name":
	case "passport":
		field = "passport_number"
	default:
		return errors.New("invalid_broadcast_profile_field")
	}
	return write(ctx, tx, owner, map[string]any{field: value})
}

func write(ctx context.Context, tx pgx.Tx, owner string, fields map[string]any) error {
	result, err := tx.Exec(ctx, `INSERT INTO core.admin_broadcast_profiles(owner,fields,overrides)
 SELECT id,jsonb_build_object('user_id',telegram_id),$2 FROM core.users WHERE id=$1
 AND (NOT EXISTS(SELECT 1 FROM core.legacy_user_references WHERE owner=$1)
 OR EXISTS(SELECT 1 FROM core.admin_broadcast_profiles WHERE owner=$1))
 ON CONFLICT(owner) DO UPDATE
 SET overrides=core.admin_broadcast_profiles.overrides || EXCLUDED.overrides`, owner, fields)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if result.RowsAffected() != 1 {
		return errors.New("broadcast_profile_missing")
	}
	return nil
}
