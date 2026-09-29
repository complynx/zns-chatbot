package adminmessage

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// DeliveryAuthorities exposes only immutable provenance needed to prelock a
// publication together with the enclosing delivery's sources.
func (s Service) DeliveryAuthorities(
	ctx context.Context,
	tx pgx.Tx,
	actor, kind string,
	id int64,
) ([]readsource.Authority, error) {
	var source sourceBinding
	var err error
	switch kind {
	case "admin_page":
		source, err = messageSource(ctx, tx, actor, id)
	case "admin_prompt", "admin_expiry":
		source, err = inputSource(ctx, tx, actor, id)
	default:
		return nil, nil
	}
	return readsource.CloneAuthorities(source.Authorities), err
}

// LockDeliveryInTx rechecks the current publication/input and its live sender.
func (s Service) LockDeliveryInTx(ctx context.Context, tx pgx.Tx, actor, kind string, id, chat int64) error {
	switch kind {
	case "admin_page":
		return guardMessage(ctx, tx, actor, id)
	case "admin_prompt", "admin_expiry":
		if _, err := guardInput(ctx, tx, actor, id); err != nil {
			return err
		}
		var allowed bool
		err := tx.QueryRow(ctx, `SELECT CASE $4 WHEN 'admin_prompt' THEN state='pending' AND expires_at>clock_timestamp() AND prompt_id=0 ELSE state IN ('pending','expired') AND expires_at<=clock_timestamp() END FROM core.admin_message_inputs WHERE id=$1 AND actor=$2 AND chat_id=$3 FOR SHARE`, id, actor, chat, kind).
			Scan(&allowed)
		if err != nil {
			return err
		}
		if !allowed {
			return rejected("admin_message_input_unavailable")
		}
		return nil
	default:
		return authorize(ctx, tx, actor)
	}
}
