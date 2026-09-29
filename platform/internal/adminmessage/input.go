package adminmessage

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

// Input is an owner-scoped intent hint, never authorization to intercept a message.
type Input struct {
	ID        int64     `json:"id"`
	ChatID    int64     `json:"chat_id"`
	PromptID  int64     `json:"prompt_id"`
	State     string    `json:"state"`
	Forward   bool      `json:"forward"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Attachment struct {
	InputID  int64  `json:"input_id"`
	ChatID   int64  `json:"chat_id"`
	PromptID int64  `json:"prompt_id"`
	Key      string `json:"key"`
}

func (s Service) BeginInput(ctx context.Context, actor, key, raw string, chat int64) (Input, error) {
	var result Input
	command, err := ParseCommand(raw)
	if err != nil {
		return result, err
	}
	if !command.NeedsInput() || chat == 0 || key == "" || len(key) > maxKeyBytes {
		return result, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return result, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.admin_message_inputs(actor,key,chat_id,command) VALUES($1,$2,$3,$4) ON CONFLICT(actor,key) DO NOTHING`,
		actor,
		key,
		chat,
		raw,
	)
	if err != nil {
		return result, err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT id,chat_id,prompt_id,state,expires_at,chat_id=$3 AND command=$4 FROM core.admin_message_inputs WHERE actor=$1 AND key=$2`, actor, key, chat, raw).
		Scan(&result.ID, &result.ChatID, &result.PromptID, &result.State, &result.ExpiresAt, &same)
	if err != nil {
		return result, err
	}
	if !same {
		return result, problem(http.StatusConflict, "idempotency_conflict")
	}
	result.Forward = command.Forward
	return result, tx.Commit(ctx)
}

// RegisterPrompt binds only the owning input to the authoritative sent prompt.
func (s Service) RegisterPrompt(ctx context.Context, actor string, id, chat, prompt int64) error {
	if prompt <= 0 {
		return invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return err
	}
	tag, err := tx.Exec(
		ctx,
		`UPDATE core.admin_message_inputs SET prompt_id=$4 WHERE id=$1 AND actor=$2 AND chat_id=$3 AND state='pending' AND expires_at>clock_timestamp() AND prompt_id IN (0,$4)`,
		id,
		actor,
		chat,
		prompt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return problem(http.StatusConflict, "admin_message_input_unavailable")
	}
	return tx.Commit(ctx)
}

// PendingInputs returns hints for this actor/chat only. Expired rows are never actionable.
func (s Service) PendingInputs(ctx context.Context, actor string, chat int64) ([]Input, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return nil, err
	}
	rows, err := tx.Query(
		ctx,
		`SELECT id,chat_id,prompt_id,state,expires_at,command FROM core.admin_message_inputs WHERE actor=$1 AND chat_id=$2 AND state='pending' AND expires_at>clock_timestamp() ORDER BY id`,
		actor,
		chat,
	)
	if err != nil {
		return nil, err
	}
	result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Input, error) {
		var value Input
		var raw string
		scanErr := row.Scan(&value.ID, &value.ChatID, &value.PromptID, &value.State, &value.ExpiresAt, &raw)
		if scanErr != nil {
			return value, scanErr
		}
		command, parseErr := ParseCommand(raw)
		value.Forward = command.Forward
		return value, parseErr
	})
	if err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

func (s Service) attachInputAttempt(ctx context.Context, actor string, input Attachment) (Message, error) {
	var result Message
	if input.Key == "" || len(input.Key) > maxKeyBytes || input.ChatID == 0 || input.PromptID <= 0 {
		return result, invalid()
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return result, err
	}
	result, err = s.attachInput(ctx, tx, actor, input)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return s.finishTemplate(ctx, actor, result)
}

func (s Service) attachInput(ctx context.Context, tx pgx.Tx, actor string, input Attachment) (Message, error) {
	var result Message
	var err error
	var raw, state string
	var oldKey *string
	var messageID *int64
	var expired bool
	err = tx.QueryRow(ctx, `SELECT command,state,attachment_key,message_id,expires_at<=clock_timestamp() FROM core.admin_message_inputs WHERE id=$1 AND actor=$2 AND chat_id=$3 AND prompt_id=$4 FOR UPDATE`, input.InputID, actor, input.ChatID, input.PromptID).
		Scan(&raw, &state, &oldKey, &messageID, &expired)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, problem(http.StatusNotFound, "not_found")
	}
	if err != nil {
		return result, err
	}
	if state == "attached" && oldKey != nil && *oldKey == input.Key && messageID != nil {
		result, err = owned(ctx, tx, actor, *messageID)
		if err != nil {
			return result, err
		}

		return result, nil
	}
	if expired || state != statePending {
		return result, problem(http.StatusConflict, "admin_message_input_unavailable")
	}
	command, err := ParseCommand(raw)
	if err != nil {
		return result, err
	}
	command.Content, err = sourceContent(ctx, tx, actor, input, command.Forward)
	if err != nil {
		return result, err
	}
	result, err = s.previewCommand(ctx, tx, actor, input.Key, raw, command)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_message_inputs SET state='attached',attachment_key=$2,message_id=$3 WHERE id=$1`,
		input.InputID,
		input.Key,
		result.ID,
	)
	if err != nil {
		return result, err
	}
	return result, nil
}

func (s Service) CancelInput(ctx context.Context, actor string, id int64) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return err
	}
	tag, err := tx.Exec(
		ctx,
		`UPDATE core.admin_message_inputs SET state='cancelled' WHERE id=$1 AND actor=$2 AND state IN ('pending','cancelled')`,
		id,
		actor,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return problem(http.StatusConflict, "admin_message_input_unavailable")
	}
	return tx.Commit(ctx)
}

// CanBroadcast exposes only the current global-role bit for filtered tool discovery.
func (s Service) CanBroadcast(ctx context.Context, actor string) (bool, error) {
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$1)`, actor).
		Scan(&allowed)
	return allowed, err
}

func (s Service) AttachInput(ctx context.Context, actor string, input Attachment) (Message, error) {
	const attempts = 3
	for range attempts {
		result, err := s.attachInputAttempt(ctx, actor, input)
		if !serializationConflict(err) {
			return result, err
		}
	}
	return Message{}, problem(http.StatusConflict, "admin_message_snapshot_busy")
}
