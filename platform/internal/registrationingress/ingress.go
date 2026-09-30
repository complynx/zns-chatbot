// Package registrationingress preserves server-observed request order before model work.
package registrationingress

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress/dbgen"
)

// Reference identifies trusted Telegram intake, not a client-provided priority.
type Reference struct {
	BotID    int64 `json:"bot_id"`
	UpdateID int64 `json:"update_id"`
}

func (r Reference) Valid() bool { return r.BotID > 0 && r.UpdateID >= 0 }

type contextKey struct{}

func WithReference(ctx context.Context, ref Reference) context.Context {
	return context.WithValue(ctx, contextKey{}, ref)
}

func FromContext(ctx context.Context) (Reference, bool) {
	ref, ok := ctx.Value(contextKey{}).(Reference)
	return ref, ok && ref.Valid()
}

// NativeBinding is transport-classified evidence, never model input.
type NativeBinding struct {
	Event   string
	Owner   string
	Payload json.RawMessage
}

// SaveTelegram shares the inbox transaction. The short lock makes committed
// positions deterministic; it is never held during authentication or model work.
func SaveTelegram(ctx context.Context, tx pgx.Tx, ref Reference, sender int64) error {
	return SaveClassifiedTelegram(ctx, tx, ref, sender, nil)
}

// SaveClassifiedTelegram inserts immutable evidence with the same inbox commit.
// A repeated update cannot replace an earlier classification.
func SaveClassifiedTelegram(ctx context.Context, tx pgx.Tx, ref Reference, sender int64, native *NativeBinding) error {
	if !ref.Valid() || sender <= 0 {
		return errors.New("registration ingress identity required")
	}
	params := dbgen.InsertIngressParams{
		BotID:      ref.BotID,
		RequestKey: strconv.FormatInt(ref.UpdateID, 10),
		Sender:     sender,
	}
	if native != nil {
		if native.Event == "" || native.Owner == "" || !json.Valid(native.Payload) {
			return errors.New("invalid native registration binding")
		}
		params.NativeEvent = pgtype.Text{String: native.Event, Valid: true}
		params.NativeOwner = pgtype.Text{String: native.Owner, Valid: true}
		params.NativePayload = native.Payload
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(782619)`); err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(dbgen.New(tx).InsertIngress(ctx, params))
}

func TelegramPosition(ctx context.Context, tx pgx.Tx, ref Reference, sender int64) (int64, error) {
	if !ref.Valid() {
		return 0, errors.New("registration ingress identity required")
	}
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM core.registration_ingress
 WHERE kind='telegram' AND bot_id=$1 AND request_key=$2 AND telegram_id=$3 AND owner=''`,
		ref.BotID, strconv.FormatInt(ref.UpdateID, 10), sender).Scan(&id)
	return id, core.DatabaseOperationError(err)
}

func ApplicationPosition(ctx context.Context, tx pgx.Tx, owner, key string) (int64, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(782619)`); err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO core.registration_ingress(kind,bot_id,request_key,owner)
 VALUES('application',0,$1,$2) ON CONFLICT(kind,bot_id,request_key,owner)
 DO UPDATE SET request_key=EXCLUDED.request_key RETURNING id`, key, owner).Scan(&id)
	return id, core.DatabaseOperationError(err)
}
