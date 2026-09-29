package botdelivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func (s Service) EnqueueCard(ctx context.Context, in CardRequest) error {
	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	if !in.Reference.Valid(in.Owner) || in.Reference.Kind != CardIntent || in.Chat <= 0 {
		return ErrBinding
	}
	owner, ref, operation, effect, child := in.Owner, in.Reference, in.Operation, in.Effect, in.Child

	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	i := Intent{
		BotID:     s.Delivery.BotID,
		Owner:     owner,
		Chat:      in.Chat,
		Reference: ref,
		Effect:    "view",
		Phase:     "send",
	}
	if in.Target > 0 {
		i.Phase, i.Target = "edit", in.Target
	}
	if err = s.lockSource(ctx, tx, i); err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		"SELECT pg_advisory_xact_lock(hashtextextended($1,81080))",
		fmt.Sprintf("%d:%s:%s:%s", i.BotID, owner, ref.Family, ref.CardKey),
	); err != nil {
		return err
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	var existing string
	err = tx.QueryRow(ctx, `SELECT operation_key FROM bot.delivery_intents WHERE bot_id=$1 AND owner=$2 AND reference=$3
 AND (($4 AND operation_key=$5 AND effect_key=$6) OR (NOT $4 AND (state IN ('pending','sending','unknown','parked','paused') OR (state='sent' AND NOT continuation_done))))
 ORDER BY created_at LIMIT 1`, i.BotID, owner, raw, child, operation, effect).Scan(&existing)
	if err == nil {
		return tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var sequence int64
	if err = tx.QueryRow(ctx, "SELECT nextval('bot.delivery_effect_ids')").Scan(&sequence); err != nil {
		return err
	}
	i.Operation = "card:" + strconv.FormatInt(sequence, 10)
	if child {
		i.Operation, i.Effect = operation, effect
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO bot.delivery_intents(bot_id,operation_key,effect_key,owner,chat_id,reference,phase,target_message_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		i.BotID,
		i.Operation,
		i.Effect,
		owner,
		i.Chat,
		raw,
		i.Phase,
		i.Target,
	)
	if err != nil {
		return err
	}
	if _, err = delivery.Register(
		ctx,
		tx,
		i.BotID,
		i.QueueReference(),
		delivery.Destination{Chat: strconv.FormatInt(i.Chat, 10)},
		delivery.Interactive,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
