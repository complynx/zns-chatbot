package bot

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// AVProcessor accepts already authorized immutable bytes, never URLs or user identity.
type AVProcessor interface {
	Preprocess(context.Context, mediaclient.Kind, []byte) (mediaproc.Result, error)
	Storyboard(context.Context, mediaclient.Kind, []byte, mediaclient.Range) (mediaproc.Result, error)
}

const avReady = "ready"
const avMillisPerSecond = 1000
const avCleanupTimeout = 3 * time.Second

func hasAV(message telegram.Message) bool {
	return message.Audio != nil || message.Voice != nil || message.Video != nil || message.VideoNote != nil
}

func (b *Bot) clearAVResults(ctx context.Context, owner string, ids []string) error {
	_, err := b.DB.Exec(ctx, `UPDATE bot.av_results a SET private_result=NULL FROM bot.media_intake m
	WHERE a.intake_id=m.id AND m.owner=$1 AND a.intake_id=ANY($2)`, owner, ids)
	return err
}

// Retire a direct spoken command after its durable reply, without touching other
// pending attachments. Its cached plan remains available for delivery retries.
func (b *Bot) finishConsumedVoice(ctx context.Context, owner string, cached cachedPlan) error {
	_, err := b.DB.Exec(ctx, `UPDATE bot.media_intake SET status='done',last_action='answer',last_origin='agent'
	WHERE owner=$1 AND id=$2 AND id=ANY($3) AND av_kind='voice' AND status='new'`, owner, cached.MediaID, cached.AVIDs)
	return err
}

func (b *Bot) resumeConsumedVoice(ctx context.Context, in incoming, updateID int64, status string) (bool, error) {
	if status != "new" && status != mediaDone {
		return false, nil
	}
	var consumed bool
	// A saved, validated spoken intent no longer needs its source audio. Replay
	// still uses the normal executor's current authorization and idempotency checks;
	// a different media target retains its own availability/selection checks.
	err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.media_intake m JOIN bot.replies r ON r.update_id=m.update_id
	WHERE m.owner=$1 AND m.id=$2 AND m.update_id=$3 AND m.av_kind='voice'
	AND r.plan->>'media_id'=m.id AND r.plan->'av_ids' ? m.id
	AND (m.status='new' OR (m.status='done' AND m.last_action='answer'))
	AND (r.plan->'plan'->>'view'<>'media' OR r.plan->'plan'->'media_action'->>'media_id'<>m.id))`, in.owner, in.mediaID, updateID).
		Scan(&consumed)
	if err != nil || !consumed {
		return false, err
	}
	return true, b.handleAgentUpdate(ctx, in, updateID)
}

func (b *Bot) purgeExpiredAV(ctx context.Context) error {
	_, err := b.DB.Exec(ctx, `UPDATE bot.av_results a SET private_result=NULL FROM bot.media_intake m
	WHERE a.intake_id=m.id AND a.private_result IS NOT NULL AND (m.expires_at<=now() OR m.status='done')`)
	return err
}

func releaseAV(conn *pgxpool.Conn, owner string) {
	ctx, cancel := context.WithTimeout(context.Background(), avCleanupTimeout)
	defer cancel()
	var unlocked bool
	err := conn.QueryRow(ctx, `SELECT pg_advisory_unlock(193827,hashtext($1))`, owner).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = conn.Hijack().Close(ctx)
		return
	}
	conn.Release()
}

func (b *Bot) prepareAV(ctx context.Context, in incoming) (bool, error) {
	var kind mediaclient.Kind
	err := b.DB.QueryRow(ctx, `SELECT av_kind FROM bot.media_intake WHERE owner=$1 AND id=$2`, in.owner, in.mediaID).
		Scan(&kind)
	if err != nil || kind == "" {
		return false, err
	}
	result, err := b.initialAV(ctx, in, kind)
	if err != nil {
		problem, denied := errors.AsType[*core.ProblemError](err)
		if errors.Is(err, pgx.ErrNoRows) || (denied && problem.Status < 500) {
			if clearErr := b.clearAVResults(ctx, in.owner, []string{in.mediaID}); clearErr != nil {
				return true, clearErr
			}
			return true, b.retireMediaView(ctx, mediaView{ID: in.mediaID, Owner: in.owner, Chat: in.chat})
		}
		return false, err
	}
	if result.Status == avReady || result.Status == "partial" {
		return false, nil
	}
	notice := i18n.AVFailed
	if result.Reason == "too_long" {
		notice = i18n.AVTooLong
	}
	if result.Status == "rejected" && result.Reason != "too_long" {
		notice = i18n.AVUnsupported
	}
	_, err = b.DB.Exec(
		ctx,
		`UPDATE bot.media_intake SET status='done',notice=$3 WHERE owner=$1 AND id=$2`,
		in.owner,
		in.mediaID,
		string(notice),
	)
	if err != nil {
		return true, err
	}
	if err = b.clearAVResults(ctx, in.owner, []string{in.mediaID}); err != nil {
		return true, err
	}
	return true, b.RenderMedia(ctx, in.owner, in.chat, in.mediaID)
}

func (b *Bot) initialAV(ctx context.Context, in incoming, kind mediaclient.Kind) (mediaproc.Result, error) {
	conn, err := b.DB.Acquire(ctx)
	if err != nil {
		return mediaproc.Result{}, err
	}
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(193827,hashtext($1))`, in.owner); err != nil {
		releaseAV(conn, in.owner)
		return mediaproc.Result{}, err
	}
	defer releaseAV(conn, in.owner)
	var attachmentID string
	err = conn.QueryRow(ctx, `SELECT attachment_id FROM bot.media_intake WHERE owner=$1 AND id=$2 AND status<>'done' AND expires_at>now()`, in.owner, in.mediaID).
		Scan(&attachmentID)
	if err != nil {
		return mediaproc.Result{}, err
	}
	attachment, err := b.API.Media(ctx, in.owner, attachmentID)
	if err != nil {
		return mediaproc.Result{}, err
	}
	var saved *mediaproc.Result
	var status string
	err = conn.QueryRow(ctx, `SELECT status,private_result FROM bot.av_results WHERE intake_id=$1`, in.mediaID).
		Scan(&status, &saved)
	if err == nil {
		if saved != nil {
			return *saved, nil
		}
		return mediaproc.Result{Status: status}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return mediaproc.Result{}, err
	}
	result := mediaproc.Result{
		Status:   "failed",
		Reason:   "worker_unavailable",
		Duration: mediaproc.Rational{Denominator: 1},
	}
	if b.AV != nil {
		result, err = b.AV.Preprocess(ctx, kind, attachment.Body)
		if err != nil {
			if ctx.Err() != nil {
				return mediaproc.Result{}, ctx.Err()
			}
			result = mediaproc.Result{
				Status:   "failed",
				Reason:   "worker_unavailable",
				Duration: mediaproc.Rational{Denominator: 1},
			}
		}
	}
	// Authorization may have changed while the external processor was running.
	if _, err = b.API.Media(ctx, in.owner, attachmentID); err != nil {
		return mediaproc.Result{}, err
	}
	command, err := conn.Exec(
		ctx,
		`INSERT INTO bot.av_results(intake_id,status,duration_num,duration_den,private_result)
	SELECT id,$2,$3,$4,$5 FROM bot.media_intake WHERE id=$1 AND owner=$6 AND status<>'done' AND expires_at>now()`,
		in.mediaID,
		result.Status,
		result.Duration.Numerator,
		result.Duration.Denominator,
		result,
		in.owner,
	)
	if err == nil && command.RowsAffected() == 0 {
		err = pgx.ErrNoRows
	}
	return result, err
}

func applyAV(input *agent.Input, id, kind string, result mediaproc.Result) {
	input.AV = &agent.AVContext{
		ID:         id,
		Kind:       kind,
		Status:     result.Status,
		Duration:   result.Duration,
		Transcript: result.Transcript,
		Sampling:   result.Sampling,
	}
	input.Attachment = nil
	input.Frames = nil
	for index, frame := range result.Frames {
		input.Frames = append(
			input.Frames,
			agent.Attachment{ID: id, Filename: fmt.Sprintf("frame-%d.jpg", index), MIME: "image/jpeg", Body: frame.JPEG,
				TimestampMS: rationalMillis(frame.Timestamp)},
		)
	}
	input.View = agent.MediaView
}

func rationalMillis(value mediaproc.Rational) int64 {
	if value.Denominator <= 0 {
		return 0
	}
	valueMS := new(big.Int).Mul(big.NewInt(value.Numerator), big.NewInt(avMillisPerSecond))
	return valueMS.Quo(valueMS, big.NewInt(value.Denominator)).Int64()
}

func (b *Bot) addAVInput(ctx context.Context, owner, id string, input *agent.Input) (bool, error) {
	var kind string
	var result *mediaproc.Result
	err := b.DB.QueryRow(ctx, `SELECT m.av_kind,a.private_result FROM bot.media_intake m
	LEFT JOIN bot.av_results a ON a.intake_id=m.id WHERE m.owner=$1 AND m.id=$2`, owner, id).Scan(&kind, &result)
	if err != nil || kind == "" {
		return false, err
	}
	if result == nil {
		return true, errors.New("private AV result unavailable")
	}
	applyAV(input, id, kind, *result)
	return true, nil
}
