package adminmessage

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Source is registered only by the authenticated transport after the user selects
// the currently received message. Ordinary user endpoints cannot create it.
type Source struct {
	Actor     string `json:"actor"`
	Key       string `json:"key"`
	ChatID    int64  `json:"chat_id"`
	MessageID int64  `json:"message_id"`
	HTML      string `json:"html"`
}

func (s Service) RegisterSource(ctx context.Context, source Source) error {
	const maxSourceBytes = 65536
	if source.Key == "" || len(source.Key) > maxKeyBytes || source.ChatID == 0 || source.MessageID <= 0 ||
		len(source.HTML) > maxSourceBytes {
		return invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, source.Actor); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.admin_message_sources(actor,key,chat_id,message_id,text_html) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`,
		source.Actor,
		source.Key,
		source.ChatID,
		source.MessageID,
		source.HTML,
	)
	if err != nil {
		return err
	}
	var same bool
	err = tx.QueryRow(ctx, `SELECT chat_id=$3 AND message_id=$4 AND text_html=$5 FROM core.admin_message_sources WHERE actor=$1 AND key=$2`, source.Actor, source.Key, source.ChatID, source.MessageID, source.HTML).
		Scan(&same)
	if err != nil {
		return err
	}
	if !same {
		return problem(http.StatusConflict, "idempotency_conflict")
	}
	return tx.Commit(ctx)
}

func sourceContent(ctx context.Context, tx pgx.Tx, actor string, input Attachment, forward bool) (Content, error) {
	var source Source
	err := tx.QueryRow(ctx, `SELECT message_id,text_html FROM core.admin_message_sources WHERE actor=$1 AND key=$2 AND chat_id=$3 AND expires_at>clock_timestamp()`, actor, input.Key, input.ChatID).
		Scan(&source.MessageID, &source.HTML)
	if errors.Is(err, pgx.ErrNoRows) {
		return Content{}, problem(http.StatusNotFound, "admin_message_source_unavailable")
	}
	if err != nil {
		return Content{}, err
	}
	if forward {
		return Content{FromChat: input.ChatID, FromMessage: source.MessageID}, nil
	}
	return Content{Text: source.HTML, ParseMode: parseHTML}, nil
}
