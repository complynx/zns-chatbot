package conversation

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (s Service) Append(ctx context.Context, actor, key, kind, text string) error {
	return s.append(ctx, actor, key, kind, text, nil)
}

// AppendAtGeneration fences derived replies against deletion in the append
// transaction. The expected generation comes from the host's saved plan.
func (s Service) AppendAtGeneration(ctx context.Context, actor, key, kind, text string, generation int64) error {
	return s.append(ctx, actor, key, kind, text, &generation)
}

func (s Service) append(ctx context.Context, actor, key, kind, text string, generation *int64) error {
	if kind != "user" && kind != "assistant" && kind != "manual" && kind != "system" {
		return errors.New("invalid conversation kind")
	}
	if len(key) == 0 || len(key) > 200 || len(text) > MaxBodyBytes || !utf8.ValidString(text) {
		return errors.New("invalid conversation event")
	}
	if err := s.knownActor(ctx, actor); err != nil {
		return err
	}
	text, omitted := sanitizePrivateText(text)
	excerpt, _ := boundText(text)
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = LockGeneration(ctx, tx, actor, generation); err != nil {
		return err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO core.conversation_events(owner,source_key,kind,text,omitted)
 VALUES($1,$2,$3,$4,$5) ON CONFLICT(owner,source_key) DO NOTHING RETURNING id`, actor, key, kind, excerpt, omitted).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	// Only a newly inserted event may gain a body. Replays cannot restore a
	// previously deleted event, and a body-write failure rolls back its excerpt.
	if !omitted && len(text) > MaxTextBytes {
		_, err = tx.Exec(ctx, `INSERT INTO core.conversation_message_bodies(event_id,body,body_sha256,character_count)
 VALUES($1,$2,encode(sha256(convert_to($2,'UTF8')),'hex'),char_length($2))`, id, text)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// LockGeneration serializes a derived write with history deletion until its
// transaction ends. The caller must first authorize the actor.
func LockGeneration(ctx context.Context, tx pgx.Tx, actor string, generation *int64) error {
	if generation == nil {
		return nil
	}
	// Use deletion's first lock before checking the generation or inserting an
	// event. Keep it through body insertion and commit.
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO core.conversation_summaries(owner) VALUES($1) ON CONFLICT DO NOTHING`,
		actor,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx,
		`SELECT version FROM core.conversation_summaries WHERE owner=$1 FOR UPDATE`,
		actor,
	); err != nil {
		return err
	}
	current, readErr := dbgen.New(tx).HistoryGeneration(ctx, actor)
	if readErr != nil {
		return readErr
	}
	if current != *generation {
		return &core.ProblemError{Status: http.StatusConflict, Code: "history_stale"}
	}
	return nil
}

// CommitSummary is a host-only operation. A conversational action cannot supply
// summary text, batch IDs, owner or coverage. CAS prevents duplicate advancement.
func (s Service) CommitSummary(ctx context.Context, actor string, version int64, ids []int64, text string) error {
	if len(ids) == 0 || len(ids) > MaxPage || len(text) > MaxSummaryBytes || !utf8.ValidString(text) ||
		strings.TrimSpace(text) == "" {
		return errors.New("invalid conversation summary")
	}
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
	var current, through int64
	if err = tx.QueryRow(ctx, `SELECT version,through_id FROM core.conversation_summaries WHERE owner=$1 FOR UPDATE`, actor).
		Scan(&current, &through); err != nil {
		return err
	}
	if current != version {
		return errors.New("conversation summary changed")
	}
	for _, id := range ids {
		if id > through {
			through = id
		}
	}
	tag, err := tx.Exec(
		ctx,
		`UPDATE core.conversation_events SET summarized_version=$3 WHERE owner=$1 AND id=ANY($2) AND summarized_version=0
 AND NOT EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=conversation_events.id)`,
		actor,
		ids,
		version+1,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != int64(len(ids)) {
		return errors.New("invalid conversation coverage")
	}
	clean, omitted := Sanitize(text)
	if omitted || len(clean) > MaxSummaryBytes {
		return errors.New("sensitive or invalid conversation summary")
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.conversation_summaries SET version=$2,through_id=$3,text=$4 WHERE owner=$1`,
		actor,
		version+1,
		through,
		clean,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}
