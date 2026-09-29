package conversation

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// Generation fences cached history and derived model outputs after deletion.
func (s Service) Generation(ctx context.Context, actor string) (int64, error) {
	if err := s.knownActor(ctx, actor); err != nil {
		return 0, err
	}
	return dbgen.New(s.DB).HistoryGeneration(ctx, actor)
}

// ReadText uses character offsets and reads only a bounded slice in PostgreSQL.
// The event identifier and digest are navigation, never authorization.
func (s Service) ReadText(
	ctx context.Context,
	actor string,
	id int64,
	offset, limit int,
	digest string,
) (TextChunk, error) {
	if id <= 0 || offset < 0 || offset > MaxBodyBytes || limit < 1 || limit > MaxChunkCharacters {
		return TextChunk{}, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidHistory}
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return TextChunk{}, err
	}
	row, err := dbgen.New(s.DB).
		ReadText(ctx, dbgen.ReadTextParams{Owner: actor, EventID: id, CharacterOffset: int32(offset), CharacterLimit: int32(limit)})
	if errors.Is(err, pgx.ErrNoRows) {
		return TextChunk{}, &core.ProblemError{Status: http.StatusNotFound, Code: "history_missing"}
	}
	if err != nil {
		return TextChunk{}, err
	}
	if digest != "" && digest != row.Digest {
		return TextChunk{}, &core.ProblemError{Status: http.StatusConflict, Code: "history_stale"}
	}
	if offset > int(row.Total) {
		return TextChunk{}, &core.ProblemError{Status: http.StatusBadRequest, Code: invalidHistory}
	}
	next := min(offset+limit, int(row.Total))
	return TextChunk{
		EventID:    id,
		Digest:     row.Digest,
		Offset:     offset,
		Total:      int(row.Total),
		Text:       row.Text,
		More:       next < int(row.Total),
		NextOffset: next,
		Generation: row.Generation,
		Omitted:    row.Omitted,
	}, nil
}

// DeleteContent is a host-only tombstone operation. Counts, chronology and event
// identity survive; durable references prohibit snapshot replay resurrection.
func (s Service) DeleteContent(ctx context.Context, actor string, id int64) error {
	if err := s.knownActor(ctx, actor); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO core.conversation_summaries(owner) VALUES($1) ON CONFLICT DO NOTHING`,
		actor,
	); err != nil {
		return err
	}
	// Same first lock as CommitSummary: no in-flight summary can revive content.
	if _, err = tx.Exec(
		ctx,
		`SELECT version FROM core.conversation_summaries WHERE owner=$1 FOR UPDATE`,
		actor,
	); err != nil {
		return err
	}
	var omitted bool
	err = tx.QueryRow(ctx, `SELECT omission_reason='deleted' FROM core.conversation_events WHERE owner=$1 AND id=$2 FOR UPDATE`, actor, id).
		Scan(&omitted)
	if errors.Is(err, pgx.ErrNoRows) {
		return &core.ProblemError{Status: http.StatusNotFound, Code: "history_missing"}
	}
	if err != nil {
		return err
	}
	if omitted {
		return tx.Commit(ctx)
	}
	batch := new(pgx.Batch)
	batch.Queue(
		`UPDATE core.conversation_events SET text='',details='{}',omitted=true,omission_reason='deleted' WHERE owner=$1 AND id=$2`,
		actor,
		id,
	)
	batch.Queue(`DELETE FROM core.conversation_message_bodies WHERE event_id=$1`, id)
	batch.Queue(`UPDATE core.legacy_message_references SET tombstoned=true WHERE event_id=$1 AND owner=$2`, id, actor)
	batch.Queue(`UPDATE core.conversation_events SET summarized_version=0 WHERE owner=$1`, actor)
	batch.Queue(`UPDATE core.conversation_summaries SET version=version+1,through_id=0,text='' WHERE owner=$1`, actor)
	batch.Queue(
		`INSERT INTO core.conversation_history_generations(owner,generation) VALUES($1,1) ON CONFLICT(owner) DO UPDATE SET generation=core.conversation_history_generations.generation+1`,
		actor,
	)
	if err = tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
