package adminmessage

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// PreviewCommand serializes the request key before resolving the audience, so
// retries return the original reviewed snapshot even after membership changes.
func (s Service) previewCommandAttempt(
	ctx context.Context,
	actor, key, raw string,
	source sourceBinding,
) (Message, error) {
	command, err := ParseCommand(raw)
	if err != nil {
		return Message{}, err
	}
	if command.NeedsInput() {
		return Message{}, problem(http.StatusConflict, "admin_message_input_required")
	}
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = prelockMessageKey(ctx, tx, actor, key, source); err != nil {
		return Message{}, err
	}
	result, err := s.previewCommand(ctx, tx, actor, key, raw, command, source)
	if err != nil {
		return result, preserveSourceRefusal(ctx, tx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	return s.finishTemplate(ctx, actor, result)
}

func (s Service) previewCommand(
	ctx context.Context,
	tx pgx.Tx,
	actor, key, raw string,
	command Command,
	source sourceBinding,
) (Message, error) {
	var result Message
	if key == "" || len(key) > maxKeyBytes {
		return result, invalid()
	}
	if err := authorize(ctx, tx, actor); err != nil {
		return result, err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, actor+":"+key); err != nil {
		return result, err
	}
	result, found, err := replayPreviewCommand(ctx, tx, actor, key, raw)
	if err != nil || found {
		return result, err
	}

	if err = fenceNewSource(ctx, tx, actor, source); err != nil {
		return Message{}, err
	}
	request := Request{Content: command.Content}
	for _, expression := range command.Recipients {
		destinations, resolveErr := s.resolveCommandRecipients(ctx, tx, actor, expression)
		if resolveErr != nil {
			return result, resolveErr
		}
		request.Destinations = append(request.Destinations, destinations...)
	}
	request, err = normalize(request)
	if err != nil {
		return result, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return result, err
	}
	result, err = previewRequest(ctx, tx, actor, key, payload)
	if err != nil {
		return result, err
	}
	if err = saveMessageSource(ctx, tx, result.ID, source); err != nil {
		return Message{}, err
	}
	if command.Template {
		if err = s.snapshotRecipientProfiles(ctx, tx, result); err != nil {
			return result, err
		}
	}
	if command.Template {
		result.State = statePreparing
	}
	_, err = tx.Exec(ctx, `UPDATE core.admin_messages SET command=$2 WHERE id=$1`, result.ID, raw)
	return result, err
}

func replayPreviewCommand(ctx context.Context, tx pgx.Tx, actor, key, raw string) (Message, bool, error) {
	var result Message
	var oldCommand *string
	err := tx.QueryRow(ctx, `SELECT id,state,request,command FROM core.admin_messages WHERE actor=$1 AND key=$2`, actor, key).
		Scan(&result.ID, &result.State, &result.Request, &oldCommand)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return Message{}, false, err
	}
	if err = guardMessage(ctx, tx, actor, result.ID); err != nil {
		return Message{}, true, err
	}
	if oldCommand == nil || *oldCommand != raw {
		return Message{}, true, problem(http.StatusConflict, "idempotency_conflict")
	}
	return result, true, nil
}

func (s Service) resolveCommandRecipients(ctx context.Context, tx pgx.Tx, actor, raw string) ([]Destination, error) {
	if strings.HasPrefix(raw, "{") {
		return s.resolveSelector(ctx, tx, raw)
	}
	if !strings.HasPrefix(raw, "$") {
		return s.commandRecipients(ctx, actor, raw)
	}
	event, category, found := strings.Cut(strings.TrimPrefix(raw, "$"), ":")
	if !found {
		category = audienceAssigned
	}
	category = strings.ToLower(strings.TrimSpace(category))
	if event == "" {
		return nil, invalid()
	}
	switch category {
	case audienceAdmins, "paid", audienceAssigned, "unpaid", "waitlist", "all":
	default:
		return nil, invalid()
	}
	return resolveShortcut(ctx, tx, event, category)
}

func (s Service) previewWithSource(ctx context.Context, actor, key, raw string, source sourceBinding) (Message, error) {
	const attempts = 3
	for range attempts {
		result, err := s.previewCommandAttempt(ctx, actor, key, raw, source)
		if !serializationConflict(err) {
			return result, err
		}
	}
	return Message{}, problem(http.StatusConflict, "admin_message_snapshot_busy")
}

func serializationConflict(err error) bool {
	if errors.Is(err, core.ErrDatabaseSerialization) {
		return true
	}
	databaseError, ok := errors.AsType[*pgconn.PgError](err)
	return ok && databaseError.Code == "40001"
}

func (s Service) PreviewCommand(ctx context.Context, actor, key, raw string) (Message, error) {
	return s.previewWithSource(ctx, actor, key, raw, originalSource())
}

func (s Service) PreviewDerivedCommand(
	ctx context.Context,
	actor, key, raw string,
	source readsource.Derivation,
) (Message, error) {
	binding, err := captureSource(actor, source)
	if err != nil {
		return Message{}, err
	}
	return s.previewWithSource(ctx, actor, key, raw, binding)
}
