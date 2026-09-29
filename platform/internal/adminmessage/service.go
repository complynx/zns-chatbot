package adminmessage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

func authorize(ctx context.Context, tx pgx.Tx, actor string) error {
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`, actor).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return problem(http.StatusForbidden, "forbidden")
	}
	return err
}

// Preview persists an immutable snapshot. Reusing a key with different content fails.
func (s Service) Preview(ctx context.Context, actor, key string, request Request) (Message, error) {
	var result Message
	if key == "" || len(key) > maxKeyBytes {
		return result, invalid()
	}
	normalized, err := normalize(request)
	if err != nil {
		return result, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return result, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = prelockMessageKey(ctx, tx, actor, key, originalSource()); err != nil {
		return result, err
	}
	var oldID int64
	lookupErr := tx.QueryRow(ctx, `SELECT id FROM core.admin_messages WHERE actor=$1 AND key=$2`, actor, key).
		Scan(&oldID)
	if lookupErr == nil {
		if err = guardMessage(ctx, tx, actor, oldID); err != nil {
			return Message{}, preserveSourceRefusal(ctx, tx, err)
		}
	} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return result, lookupErr
	}
	if err = authorize(ctx, tx, actor); err != nil {
		return result, err
	}
	result, err = previewRequest(ctx, tx, actor, key, payload)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func previewRequest(ctx context.Context, tx pgx.Tx, actor, key string, payload []byte) (Message, error) {
	var result Message
	var err error
	// Serialize retries of the same request before inspecting its durable snapshot.
	_, err = tx.Exec(ctx, `INSERT INTO core.admin_messages(actor,key,request) VALUES($1,$2,$3)
 ON CONFLICT(actor,key) DO NOTHING`, actor, key, payload)
	if err != nil {
		return result, err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT id,state,request,request=$3::jsonb FROM core.admin_messages
 WHERE actor=$1 AND key=$2`, actor, key, payload).Scan(&result.ID, &result.State, &result.Request, &same)
	if err != nil {
		return result, err
	}
	if !same {
		return result, problem(http.StatusConflict, "idempotency_conflict")
	}
	_, err = tx.Exec(ctx, `INSERT INTO core.admin_message_recipients(message_id,position,destination,content)
 SELECT m.id,r.position,r.destination,m.request->'content' FROM core.admin_messages m
 CROSS JOIN LATERAL jsonb_array_elements(m.request->'destinations') WITH ORDINALITY AS r(destination,position)
 WHERE m.id=$1 ON CONFLICT DO NOTHING`, result.ID)
	if err != nil {
		return result, err
	}
	return result, nil
}

// Enqueue requires the owning administrator's explicit action on a saved preview.
func (s Service) Enqueue(ctx context.Context, actor string, id int64) error {
	bindings, err := s.publicationBindings(ctx, actor, id)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message, err := owned(ctx, tx, actor, id)
	if err != nil {
		return preserveSourceRefusal(ctx, tx, err)
	}
	if message.State == statePreparing {
		return problem(http.StatusConflict, "admin_message_preparing")
	}
	if message.State == stateCancelled {
		return problem(http.StatusConflict, "admin_message_cancelled")
	}
	var failures bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.admin_message_recipients WHERE message_id=$1 AND failure<>'')`, id).
		Scan(&failures); err != nil {
		return err
	}
	if failures {
		return problem(http.StatusConflict, "admin_message_render_failed")
	}
	err = s.enqueueRegistered(ctx, tx, id, bindings)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE core.admin_messages SET state='queued' WHERE id=$1`, id)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func owned(ctx context.Context, tx pgx.Tx, actor string, id int64) (Message, error) {
	if err := guardMessage(ctx, tx, actor, id); err != nil {
		return Message{}, err
	}
	var result Message
	if err := authorize(ctx, tx, actor); err != nil {
		return result, err
	}
	err := tx.QueryRow(ctx, `SELECT id,state,request FROM core.admin_messages WHERE id=$1 AND actor=$2 FOR UPDATE`, id, actor).
		Scan(&result.ID, &result.State, &result.Request)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, problem(http.StatusNotFound, "not_found")
	}
	return result, err
}

// Cancel stops unclaimed deliveries. An in-flight Telegram request cannot be recalled.
func (s Service) Cancel(ctx context.Context, actor string, id int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = owned(ctx, tx, actor, id); err != nil {
		return preserveSourceRefusal(ctx, tx, err)
	}
	items, err := lockAdminPublication(ctx, tx, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE core.admin_messages SET state='cancelled' WHERE id=$1`, id)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE core.admin_message_deliveries SET state='cancelled'
 WHERE message_id=$1 AND state='pending'`, id)
	if err == nil {
		err = projectAdminCancellation(ctx, tx, items)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) Results(ctx context.Context, actor string, id int64) ([]Delivery, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = owned(ctx, tx, actor, id); err != nil {
		return nil, preserveSourceRefusal(ctx, tx, err)
	}
	rows, err := tx.Query(
		ctx,
		`SELECT d.id,d.message_id,d.destination,COALESCE(d.content,m.request->'content'),d.state,d.attempt,d.telegram_message_id,d.failure
 FROM core.admin_message_deliveries d JOIN core.admin_messages m ON m.id=d.message_id WHERE d.message_id=$1 ORDER BY d.id`,
		id,
	)
	if err != nil {
		return nil, err
	}
	result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Delivery, error) {
		var delivery Delivery
		scanErr := row.Scan(
			&delivery.ID,
			&delivery.MessageID,
			&delivery.Destination,
			&delivery.Content,
			&delivery.State,
			&delivery.Attempt,
			&delivery.TelegramMessageID,
			&delivery.Failure,
		)
		return delivery, scanErr
	})
	if err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}
