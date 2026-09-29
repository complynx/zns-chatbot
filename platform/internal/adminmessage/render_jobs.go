package adminmessage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

// snapshotRecipientProfiles freezes all rendering data before any model call.
func (s Service) snapshotRecipientProfiles(ctx context.Context, tx pgx.Tx, message Message) error {
	if _, err := parseBroadcastTemplate(message.Request.Content); err != nil {
		return err
	}
	for position, destination := range message.Request.Destinations {
		fields, profileErr := s.broadcastProfile(ctx, tx, destination)
		failure := ""
		state := statePending
		var owner *string
		if profileErr != nil {
			if !strings.HasPrefix(profileErr.Error(), "template_") {
				return profileErr
			}
			failure = profileErr.Error()
			state = "done"
		} else {
			err := tx.QueryRow(ctx, `SELECT id FROM core.users WHERE telegram_id::text=$1 OR ($1 LIKE '@%' AND lower(username)=lower(substr($1,2)))`, destination.Chat).
				Scan(&owner)
			if err != nil {
				return err
			}
		}
		_, err := tx.Exec(
			ctx,
			`UPDATE core.admin_message_recipients SET profile=$3,profile_owner=$4,content='{}',failure=$5,render_state=$6 WHERE message_id=$1 AND position=$2`,
			message.ID,
			position+1,
			fields,
			owner,
			failure,
			state,
		)
		if err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE core.admin_messages SET state='preparing' WHERE id=$1`, message.ID)
	return err
}

const broadcastNameTimeout = 30 * time.Second

type renderJob struct {
	Position, Attempt int64
	Owner             string
	Fields            map[string]any
	Content           Content
}

func (s Service) claimRender(ctx context.Context, actor string, id int64) (renderJob, bool, error) {
	var job renderJob
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return job, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message, err := owned(ctx, tx, actor, id)
	if err != nil {
		return job, false, preserveSourceRefusal(ctx, tx, err)
	}
	if message.State != statePreparing {
		return job, false, nil
	}
	job.Content = message.Request.Content
	var profile []byte
	err = tx.QueryRow(ctx, `SELECT position,render_attempt+1,profile_owner,profile FROM core.admin_message_recipients WHERE message_id=$1 AND (render_state='pending' OR (render_state='rendering' AND render_available_at<=clock_timestamp())) ORDER BY position LIMIT 1 FOR UPDATE SKIP LOCKED`, id).
		Scan(&job.Position, &job.Attempt, &job.Owner, &profile)
	if errors.Is(err, pgx.ErrNoRows) {
		return job, false, tx.Commit(ctx)
	}
	if err != nil {
		return job, false, err
	}
	job.Fields, err = decodeBroadcastProfile(profile)
	if err != nil {
		return job, false, err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_message_recipients SET render_state='rendering',render_attempt=$3,render_available_at=clock_timestamp()+interval '2 minutes' WHERE message_id=$1 AND position=$2`,
		id,
		job.Position,
		job.Attempt,
	)
	if err != nil {
		return job, false, err
	}
	return job, true, tx.Commit(ctx)
}

func (s Service) finishTemplate(ctx context.Context, actor string, message Message) (Message, error) {
	if message.State != statePreparing {
		return message, s.CheckPublication(ctx, actor, message.ID)
	}
	renderer, err := parseBroadcastTemplate(message.Request.Content)
	if err != nil {
		return message, err
	}
	for {
		job, found, claimErr := s.claimRender(ctx, actor, message.ID)
		if claimErr != nil {
			return message, claimErr
		}
		if !found {
			break
		}
		failure := ""
		content := Content{}
		nameCtx := credits.WithScope(
			ctx,
			credits.Scope{
				Actor: actor,
				Payer: actor,
				Key:   fmt.Sprintf("broadcast:%d:%d:%d", message.ID, job.Position, job.Attempt),
			},
		)
		if err = s.ensureInformalName(nameCtx, actor, message.ID, &job); err != nil {
			failure = "admin_message_informal_name_unavailable"
		} else {
			content, err = renderer.render(job.Fields, job.Content)
			if err != nil {
				failure = "admin_message_template_render_failed"
			}
		}
		if ctx.Err() != nil {
			return message, ctx.Err()
		}
		err = s.persistRender(ctx, actor, message.ID, job, content, failure)
		if err != nil {
			return message, err
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return message, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = owned(ctx, tx, actor, message.ID); err != nil {
		return message, preserveSourceRefusal(ctx, tx, err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_messages SET state='draft' WHERE id=$1 AND state='preparing' AND NOT EXISTS(SELECT 1 FROM core.admin_message_recipients WHERE message_id=$1 AND render_state<>'done')`,
		message.ID,
	)
	if err != nil {
		return message, err
	}
	err = tx.QueryRow(ctx, `SELECT state FROM core.admin_messages WHERE id=$1`, message.ID).Scan(&message.State)
	if err != nil {
		return message, err
	}
	return message, tx.Commit(ctx)
}

func (s Service) ensureInformalName(ctx context.Context, actor string, id int64, job *renderJob) error {
	if value, present := job.Fields["informal_name"]; present {
		job.Fields["user_informal_name"] = value
		return nil
	}
	if s.InformalName == nil {
		return rejected("admin_message_informal_name_unavailable")
	}
	names := make(map[string]any)
	for key, value := range job.Fields {
		if strings.Contains(key, "name") && key != "bot_username" && key != "user_name" &&
			key != "user_informal_name" &&
			key != "user_link_username" {
			names[key] = value
		}
	}
	// The callback runs after the claim transaction commits, never under row locks.
	callCtx, cancel := context.WithTimeout(ctx, broadcastNameTimeout)
	defer cancel()
	if err := s.CheckPublication(callCtx, actor, id); err != nil {
		return err
	}
	value, err := s.InformalName(callCtx, names)
	if err != nil {
		return err
	}
	if value == "" {
		job.Fields["user_informal_name"] = ""
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = guardMessage(ctx, tx, actor, id); err != nil {
		return preserveSourceRefusal(ctx, tx, err)
	}
	updated, err := tx.Exec(
		ctx,
		`UPDATE core.admin_broadcast_profiles SET overrides=CASE WHEN (fields||overrides) ? 'informal_name' THEN overrides ELSE overrides||jsonb_build_object('informal_name',$2::text) END WHERE owner=$1`,
		job.Owner,
		value,
	)
	if err != nil {
		return err
	}
	if updated.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	// Cache writes preserve explicit edits; this draft keeps its own generated value.
	job.Fields["user_informal_name"] = value
	return nil
}

// Rendering happens outside locks; persistence rechecks the retained source.
func (s Service) persistRender(
	ctx context.Context,
	actor string,
	id int64,
	job renderJob,
	content Content,
	failure string,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = guardMessage(ctx, tx, actor, id); err != nil {
		return preserveSourceRefusal(ctx, tx, err)
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_message_recipients SET content=$4,failure=$5,render_state='done' WHERE message_id=$1 AND position=$2 AND render_attempt=$3 AND render_state='rendering'`,
		id,
		job.Position,
		job.Attempt,
		content,
		failure,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResumePreview continues only an existing immutable preparation snapshot.
func (s Service) ResumePreview(ctx context.Context, actor string, id int64) (Message, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	message, err := owned(ctx, tx, actor, id)
	if err != nil {
		return message, preserveSourceRefusal(ctx, tx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return message, err
	}
	return s.finishTemplate(ctx, actor, message)
}
