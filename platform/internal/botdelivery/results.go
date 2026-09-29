package botdelivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (s Service) EnqueueResult(ctx context.Context, in ResultRequest) error {
	owner, chat, update, effect := in.Owner, in.Chat, in.Update, in.Effect

	if err := s.Delivery.Validate(); err != nil {
		return err
	}
	operation, effectKey := ResultOperation(owner, update, effect)
	key := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effectKey}
	existing, err := Read(ctx, s.DB, s.Delivery.BotID, key, false)
	if err == nil {
		if existing.Owner != owner || existing.Chat != chat {
			return ErrBinding
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = s.enqueueResultTx(ctx, tx, in); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
func (s Service) enqueueResultTx(ctx context.Context, tx pgx.Tx, in ResultRequest) error {
	owner, chat, update, effect, ref, result, target := in.Owner, in.Chat, in.Update, in.Effect, in.Reference, in.Result, in.Target
	operation, effectKey := ResultOperation(owner, update, effect)
	key := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effectKey}
	var err error
	if ref.Source != nil && !reflect.DeepEqual(ref.Source, result.Source) {
		return ErrBinding
	}

	if err = bindBotResultGeneration(ctx, tx, owner, ref, &result); err != nil {
		return err
	}
	ref.Kind, ref.Update, ref.ResultKind = ResultIntent, update, "delivery_result:"+effectKey
	ref.Generation, ref.Source = &result.Generation, result.Source
	if !ref.Valid(owner) {
		return ErrBinding
	}
	phase := "send"
	if target > 0 {
		phase = "edit"
	}
	i := Intent{
		BotID:     s.Delivery.BotID,
		Operation: operation,
		Effect:    effectKey,
		Owner:     owner,
		Chat:      chat,
		Reference: ref,
		Phase:     phase,
		Target:    target,
	}
	if err = s.lockSource(ctx, tx, i); err != nil {
		return err
	}
	if err = storeBotResult(ctx, tx, i, result); err != nil {
		return err
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO bot.delivery_intents(bot_id,operation_key,effect_key,owner,chat_id,reference,phase,target_message_id)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`,
		i.BotID,
		operation,
		effectKey,
		owner,
		chat,
		raw,
		phase,
		target,
	)
	if err != nil {
		return err
	}
	current, err := Read(ctx, tx, i.BotID, key, true)
	if err != nil {
		return err
	}
	if current.Owner != owner || current.Chat != chat || !reflect.DeepEqual(current.Reference, ref) {
		return ErrBinding
	}
	if _, err = delivery.Register(
		ctx,
		tx,
		i.BotID,
		key,
		delivery.Destination{Chat: strconv.FormatInt(chat, 10)},
		delivery.Interactive,
	); err != nil {
		return err
	}
	return nil
}
func ResultOperation(owner string, update int64, effect string) (string, string) {
	actor := sha256.Sum256([]byte(owner))
	digest := sha256.Sum256([]byte(effect))
	return "reply:" + hex.EncodeToString(
			actor[:16],
		) + ":" + strconv.FormatInt(
			update,
			10,
		), hex.EncodeToString(
			digest[:],
		)
}
func bindBotResultGeneration(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	ref Reference,
	result *StoredResult,
) error {
	if result.Source != nil {
		if !result.Source.Valid() {
			return ErrBinding
		}
		result.Generation = *result.Source.Generation
		if ref.Generation != nil && *ref.Generation != result.Generation {
			return ErrBinding
		}
	} else if ref.Generation != nil {
		result.Generation = *ref.Generation
	} else if err := tx.QueryRow(ctx, "SELECT COALESCE((SELECT generation FROM core.conversation_history_generations WHERE owner=$1),0)", owner).Scan(&result.Generation); err != nil {
		return err
	}
	return nil
}
func storeBotResult(ctx context.Context, tx pgx.Tx, i Intent, result StoredResult) error {
	result.Payload.ChatID = i.Chat
	if result.Notice == "" {
		var err error
		result.Payload, err = telegram.PrepareSend(result.Payload)
		if err != nil {
			return err
		}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(raw) > 1<<20 {
		return ErrBinding
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`,
		i.Owner,
		i.Reference.Update,
		i.Reference.ResultKind,
		raw,
	)
	if err != nil {
		return err
	}
	var stored StoredResult
	if err = tx.QueryRow(ctx, "SELECT content FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind=$3", i.Owner, i.Reference.Update, i.Reference.ResultKind).
		Scan(&stored); err != nil {
		return err
	}
	if stored.Generation != result.Generation || !reflect.DeepEqual(stored.Source, result.Source) {
		return ErrStale
	}
	return nil
}
