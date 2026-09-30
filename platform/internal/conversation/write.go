package conversation

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/conversation/fence"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// AppendOriginal records source correspondence. Imported assistant messages are
// original evidence too; the role does not determine their provenance.
func (s Service) AppendOriginal(ctx context.Context, actor, key, kind, text string) error {
	if kind != "user" && kind != "assistant" && kind != "manual" {
		return errors.New("invalid original conversation kind")
	}
	return s.append(ctx, actor, key, kind, text, "original", nil, nil)
}

// AppendTrustedOutcome records host-rendered notices and domain outcomes. Callers
// must supply their authoritative rendering, never model-generated reply text.
func (s Service) AppendTrustedOutcome(ctx context.Context, actor, key, text string) error {
	return s.append(ctx, actor, key, "system", text, "trusted", nil, nil)
}

// AppendDerived retains bounded host provenance and validates it while source
// locks are held through the archive commit.
func (s Service) AppendDerived(
	ctx context.Context,
	actor, key, text string,
	generation int64,
	authorities []readsource.Authority,
) error {
	if generation < 0 || authorities == nil || !readsource.Valid(authorities) {
		return errors.New("invalid derived conversation provenance")
	}
	return s.append(ctx, actor, key, "assistant", text, "derived", &generation, authorities)
}

func (s Service) append(
	ctx context.Context,
	actor, key, kind, text, origin string,
	generation *int64,
	authorities []readsource.Authority,
) error {
	if len(key) == 0 || len(key) > 200 || len(text) > MaxBodyBytes || !utf8.ValidString(text) {
		return errors.New("invalid conversation event")
	}
	text, omitted := sanitizePrivateText(text)
	excerpt, _ := boundText(text)
	state, err := s.authoritySnapshot(ctx, actor, authorities, authorityScope{})
	if err != nil {
		return err
	}
	tx := state.tx
	defer func() { _ = tx.Rollback(ctx) }()
	if !state.allowed {
		return staleHistory()
	}
	if err = LockGeneration(ctx, tx, actor, generation); err != nil {
		return err
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO core.conversation_events(owner,source_key,kind,text,omitted,origin)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(owner,source_key) DO NOTHING RETURNING id`, actor, key, kind, excerpt, omitted, origin).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationError(tx.Commit(ctx))
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if authorities != nil {
		if _, err = tx.Exec(
			ctx,
			`INSERT INTO core.conversation_read_authorities(event_id,authorities,history_generation) VALUES($1,$2,$3)`,
			id,
			authorities,
			*generation,
		); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	// Only a newly inserted event may gain a body. Replays cannot restore a
	// previously deleted event, and a body-write failure rolls back its excerpt.
	if !omitted && len(text) > MaxTextBytes {
		_, err = tx.Exec(ctx, `INSERT INTO core.conversation_message_bodies(event_id,body,body_sha256,character_count)
 VALUES($1,$2,encode(sha256(convert_to($2,'UTF8')),'hex'),char_length($2))`, id, text)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

// LockGeneration retains the public conversation fence for existing callers.
func LockGeneration(ctx context.Context, tx pgx.Tx, actor string, generation *int64) error {
	return fence.LockGeneration(ctx, tx, actor, generation)
}

// CommitSummary is a host-only operation. A conversational action cannot supply
// summary text, batch IDs, owner or coverage. CAS prevents duplicate advancement.
func (s Service) CommitSummary(ctx context.Context, actor string, version int64, ids []int64, text string) error {
	if len(ids) == 0 || len(ids) > MaxPage || len(text) > MaxSummaryBytes || !utf8.ValidString(text) ||
		strings.TrimSpace(text) == "" {
		return errors.New("invalid conversation summary")
	}
	state, err := s.authoritySnapshot(ctx, actor, nil, authorityScope{ids: ids, summary: true})
	if err != nil {
		return err
	}
	tx := state.tx
	defer func() { _ = tx.Rollback(ctx) }()
	authorities, err := state.summaryAuthorities(ids)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO core.conversation_summaries(owner) VALUES($1) ON CONFLICT DO NOTHING`,
		actor,
	); err != nil {
		return core.DatabaseOperationError(err)
	}
	var current, through int64
	if err = tx.QueryRow(ctx, `SELECT version,through_id FROM core.conversation_summaries WHERE owner=$1 FOR UPDATE`, actor).
		Scan(&current, &through); err != nil {
		return core.DatabaseOperationError(err)
	}
	if current != version {
		if err = tx.Commit(ctx); err != nil {
			return core.DatabaseOperationError(err)
		}
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
		return core.DatabaseOperationError(err)
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
		`UPDATE core.conversation_summaries SET version=$2,through_id=$3,text=$4,read_authorities=$5 WHERE owner=$1`,
		actor,
		version+1,
		through,
		clean,
		append([]readsource.Authority{}, authorities...),
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}
