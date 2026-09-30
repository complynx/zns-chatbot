package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
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
	return core.DatabaseOperationError(err)
}

// Retire a direct spoken command after its durable reply, without touching other
// pending attachments. Its cached plan remains available for delivery retries.
func (b *Bot) finishConsumedVoice(ctx context.Context, owner string, cached interaction.SavedPlan) error {
	_, err := b.DB.Exec(ctx, `UPDATE bot.media_intake SET status='done',last_action='answer',last_origin='agent'
	WHERE owner=$1 AND id=$2 AND id=ANY($3) AND av_kind='voice' AND status='new'`, owner, cached.MediaID, cached.AVIDs)
	return core.DatabaseOperationError(err)
}

func (b *Bot) resumeConsumedVoice(ctx context.Context, in incoming, updateID int64, status string) (bool, error) {
	if status != "new" && status != mediaDone {
		return false, nil
	}
	// Replaying a saved spoken intent still checks current authority and receipts.
	consumed, err := (interaction.Store{DB: b.DB}).ConsumedVoice(ctx, in.owner, in.mediaID, updateID)
	if err != nil || !consumed {
		return false, err
	}
	return true, b.handleAgentUpdate(ctx, in, updateID)
}

func (b *Bot) purgeExpiredAV(ctx context.Context) error {
	_, err := b.DB.Exec(ctx, `UPDATE bot.av_results a SET private_result=NULL FROM bot.media_intake m
	WHERE a.intake_id=m.id AND a.private_result IS NOT NULL AND (m.expires_at<=now() OR m.status='done')`)
	return core.DatabaseOperationError(err)
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
		return false, core.DatabaseOperationError(err)
	}
	result, err := b.initialAV(ctx, in, kind)
	if err != nil {
		if core.IsDatabaseFailure(err) {
			return false, err
		}
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
		return true, core.DatabaseOperationError(err)
	}
	if err = b.clearAVResults(ctx, in.owner, []string{in.mediaID}); err != nil {
		return true, err
	}
	return true, b.RenderMedia(ctx, in.owner, in.chat, in.mediaID)
}

func (b *Bot) initialAV(ctx context.Context, in incoming, kind mediaclient.Kind) (mediaproc.Result, error) {
	conn, err := b.DB.Acquire(ctx)
	if err != nil {
		return mediaproc.Result{}, core.DatabaseOperationError(err)
	}
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(193827,hashtext($1))`, in.owner); err != nil {
		releaseAV(conn, in.owner)
		return mediaproc.Result{}, core.DatabaseOperationError(err)
	}
	defer releaseAV(conn, in.owner)
	var attachmentID string
	err = conn.QueryRow(ctx, `SELECT attachment_id FROM bot.media_intake WHERE owner=$1 AND id=$2 AND status<>'done' AND expires_at>now()`, in.owner, in.mediaID).
		Scan(&attachmentID)
	if err != nil {
		return mediaproc.Result{}, core.DatabaseOperationError(err)
	}
	attachment, err := b.API.Media(ctx, in.owner, attachmentID)
	if err != nil {
		return mediaproc.Result{}, err
	}
	var status string
	var raw []byte
	err = conn.QueryRow(ctx, `SELECT status,private_result FROM bot.av_results WHERE intake_id=$1`, in.mediaID).
		Scan(&status, &raw)
	if err == nil {
		saved, found, decodeErr := decodeAVResult(raw)
		if decodeErr != nil {
			return mediaproc.Result{}, decodeErr
		}
		if found {
			return saved, nil
		}
		return mediaproc.Result{Status: status}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return mediaproc.Result{}, core.DatabaseOperationError(err)
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
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	if command.RowsAffected() == 0 {
		return result, pgx.ErrNoRows
	}
	return result, nil
}

// decodeAVResult decodes a stored private result outside SQL classification:
// readable but incompatible JSON is an ordinary decode error. SQL NULL and JSON
// null both mean no saved result, as when pgx decoded into *mediaproc.Result.
func decodeAVResult(raw []byte) (mediaproc.Result, bool, error) {
	var saved *mediaproc.Result
	if raw != nil {
		if err := json.Unmarshal(raw, &saved); err != nil {
			return mediaproc.Result{}, false, fmt.Errorf("decode private AV result: %w", err)
		}
	}
	if saved == nil {
		return mediaproc.Result{}, false, nil
	}
	return *saved, true, nil
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
	var raw []byte
	err := b.DB.QueryRow(ctx, `SELECT m.av_kind,a.private_result FROM bot.media_intake m
	LEFT JOIN bot.av_results a ON a.intake_id=m.id WHERE m.owner=$1 AND m.id=$2`, owner, id).Scan(&kind, &raw)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	// Decoding precedes the kind check, as it did inside the former row scan.
	result, found, err := decodeAVResult(raw)
	if err != nil || kind == "" {
		return false, err
	}
	if !found {
		return true, errors.New("private AV result unavailable")
	}
	applyAV(input, id, kind, result)
	return true, nil
}
