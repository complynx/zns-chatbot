package bot

import (
	"context"
	"errors"
	"math/big"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/media"
	"github.com/complynx/zns-chatbot/platform/internal/mediaclient"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

func (b *Bot) addCurrentAV(ctx context.Context, in incoming, input *agent.Input) error {
	if in.mediaID == "" {
		return nil
	}
	var kind string
	err := b.DB.QueryRow(ctx, `SELECT av_kind FROM bot.media_intake WHERE owner=$1 AND id=$2`, in.owner, in.mediaID).
		Scan(&kind)
	if err != nil || kind == "" {
		return err
	}
	return b.addCurrentMedia(ctx, in, input)
}

func (b *Bot) avHint(ctx context.Context, owner string, hint *agent.MediaHint) error {
	var kind, status, attachmentID string
	var duration mediaproc.Rational
	err := b.DB.QueryRow(ctx, `SELECT m.av_kind,m.attachment_id,a.status,a.duration_num,a.duration_den
	FROM bot.media_intake m JOIN bot.av_results a ON a.intake_id=m.id
	WHERE m.owner=$1 AND m.id=$2 AND m.status<>'done' AND m.expires_at>now()`, owner, hint.ID).
		Scan(&kind, &attachmentID, &status, &duration.Numerator, &duration.Denominator)
	if errors.Is(err, pgx.ErrNoRows) {
		hint.CanInspect = false
		return nil
	}
	if err != nil {
		return err
	}
	hint.Kind = kind
	hint.DurationMS = rationalMillis(duration)
	hint.CanInspect = false
	if b.AV == nil || (kind != "video" && kind != "video_note") || (status != avReady && status != "partial") {
		return nil
	}
	var attachment media.Attachment
	err = b.API.Call(ctx, owner, http.MethodGet, "/v1/media/"+url.PathEscape(attachmentID), nil, &attachment)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return b.clearAVResults(ctx, owner, []string{hint.ID})
	}
	if err == nil {
		hint.CanInspect = true
	}
	return err
}

func (b *Bot) avInspectionContext(
	ctx context.Context,
	owner string,
	updateID int64,
) (*agent.AVInspectionContext, error) {
	var rounds int
	err := b.DB.QueryRow(ctx, `SELECT rounds FROM bot.av_refinements WHERE owner=$1 AND update_id=$2`, owner, updateID).
		Scan(&rounds)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	const maxRounds = 2
	return &agent.AVInspectionContext{Remaining: maxRounds - rounds, Completed: []agent.AVInspection{}}, nil
}

func (b *Bot) refineAV(
	ctx context.Context,
	owner string,
	updateID int64,
	proposal agent.MediaProposal,
	input *agent.Input,
) (i18n.ID, error) {
	if b.AV == nil {
		return i18n.AVFailed, nil
	}
	var kind mediaclient.Kind
	var attachmentID string
	var duration mediaproc.Rational
	err := b.DB.QueryRow(ctx, `SELECT m.av_kind,m.attachment_id,a.duration_num,a.duration_den
	FROM bot.media_intake m JOIN bot.av_results a ON a.intake_id=m.id
	WHERE m.owner=$1 AND m.id=$2 AND m.status<>'done' AND m.expires_at>now()
	AND a.status IN ('ready','partial') AND m.av_kind IN ('video','video_note')`, owner, proposal.MediaID).
		Scan(&kind, &attachmentID, &duration.Numerator, &duration.Denominator)
	if errors.Is(err, pgx.ErrNoRows) {
		return i18n.MediaUnavailable, nil
	}
	if err != nil {
		return "", err
	}
	if big.NewRat(proposal.EndMS, avMillisPerSecond).Cmp(big.NewRat(duration.Numerator, duration.Denominator)) > 0 {
		return i18n.AVRangeInvalid, nil
	}
	attachment, err := b.API.Media(ctx, owner, attachmentID)
	if err != nil {
		if problem, ok := errors.AsType[*core.ProblemError](
			err,
		); ok &&
			problem.Status < http.StatusInternalServerError {
			return i18n.MediaUnavailable, nil
		}
		return "", err
	}
	rounds, err := b.reserveAVRound(ctx, owner, updateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return i18n.AVInspectLimit, nil
	}
	if err != nil {
		return "", err
	}
	const maxRounds = 2
	input.AVInspection.Remaining = maxRounds - rounds
	result, err := b.AV.Storyboard(
		ctx,
		kind,
		attachment.Body,
		mediaclient.Range{StartMS: proposal.StartMS, EndMS: proposal.EndMS, Count: proposal.FrameCount},
	)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return i18n.AVFailed, nil
	}
	if result.Status != avReady {
		return i18n.AVFailed, nil
	}
	return b.acceptAVRange(ctx, owner, attachmentID, kind, proposal, input, result)
}

func (b *Bot) reserveAVRound(ctx context.Context, owner string, updateID int64) (int, error) {
	var rounds int
	err := b.DB.QueryRow(ctx, `INSERT INTO bot.av_refinements(owner,update_id,rounds) VALUES($1,$2,1)
	ON CONFLICT(owner,update_id) DO UPDATE SET rounds=bot.av_refinements.rounds+1
	WHERE bot.av_refinements.rounds<2 RETURNING rounds`, owner, updateID).Scan(&rounds)
	return rounds, err
}

func (b *Bot) acceptAVRange(
	ctx context.Context,
	owner, attachmentID string,
	kind mediaclient.Kind,
	proposal agent.MediaProposal,
	input *agent.Input,
	result mediaproc.Result,
) (i18n.ID, error) {
	_, err := b.API.Media(ctx, owner, attachmentID)
	if err != nil {
		if problem, ok := errors.AsType[*core.ProblemError](
			err,
		); ok &&
			problem.Status < http.StatusInternalServerError {
			return i18n.MediaUnavailable, nil
		}
		return "", err
	}
	active, err := b.loadMediaIntake(ctx, owner, proposal.MediaID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && active.Status == mediaDone) {
		return i18n.MediaUnavailable, nil
	}
	if err != nil {
		return "", err
	}
	var inspected agent.Input
	applyAV(&inspected, proposal.MediaID, string(kind), result)
	// A range is additional evidence. Keep the original spoken request and all
	// earlier inspected frames so a stateless model can compare both intervals.
	if input.AV == nil {
		input.AV = inspected.AV
	}
	if input.Attachment != nil {
		input.Frames = append(input.Frames, *input.Attachment)
		input.Attachment = nil
	}
	input.Frames = append(input.Frames, inspected.Frames...)
	input.AVInspection.Completed = append(input.AVInspection.Completed, agent.AVInspection{
		MediaID: proposal.MediaID, StartMS: proposal.StartMS, EndMS: proposal.EndMS,
		FrameCount: len(inspected.Frames),
	})
	return "", nil
}
