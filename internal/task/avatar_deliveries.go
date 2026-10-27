package task

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type avatarDeliveryArgs struct {
	DeliveryID uuid.UUID `json:"delivery_id"`
	Version    int64     `json:"version"`
}

func (avatarDeliveryArgs) Kind() string { return "zinusya_avatar_delivery" }

type avatarDeliveryWorker struct {
	river.WorkerDefaults[avatarDeliveryArgs]

	service *Service
}

func (w *avatarDeliveryWorker) Work(ctx context.Context, job *river.Job[avatarDeliveryArgs]) error {
	started := time.Now()
	err := w.service.deliverAvatar(ctx, job.Args, job.ID)
	w.service.recordWorkerDuration(ctx, job.Args.Kind(), err, time.Since(started))
	return err
}
func (s *Service) repairAvatarDeliveries(ctx context.Context) {
	batch, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	jobs, err := s.store.ReadyAvatarDeliveries(batch, s.notificationScope(), s.avatarDeliveryCursor)
	if err == nil {
		for _, job := range jobs {
			_, err = s.queue.Insert(
				batch,
				avatarDeliveryArgs{DeliveryID: job.ID, Version: job.Version},
				&river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}},
			)
			if err != nil {
				break
			}
			s.avatarDeliveryCursor = job.ID
		}
		if err == nil && len(jobs) < db.NotificationRepairPageSize {
			s.avatarDeliveryCursor = uuid.Nil
		}
	}
	if err != nil && ctx.Err() == nil {
		s.record(ctx, "avatar_deliveries", metricError)
		s.logger.WarnContext(ctx, "avatar delivery scheduling unavailable", "class", "avatar_delivery_queue")
	}
}

// Fixed stripes bound memory and prevent a rescued job from reconciling a
// still-active send in this process. A collision snoozes, never completes work.
const avatarDeliveryLockCount = 64

// The non-secret attempt fence binds a reservation to its River job. Retrying
// that job can reconcile a failed outcome write; another job cannot claim it.
func avatarDeliveryAttempt(args avatarDeliveryArgs, jobID int64) uuid.UUID {
	const customUUIDVersion = 8
	name := strconv.FormatInt(jobID, 10) + ":" + strconv.FormatInt(args.Version, 10)
	return uuid.NewHash(sha256.New(), args.DeliveryID, []byte(name), customUUIDVersion)
}
func (s *Service) deliverAvatar(ctx context.Context, args avatarDeliveryArgs, jobID int64) error {
	gate := &s.avatarDeliverySend[int(args.DeliveryID[0])%avatarDeliveryLockCount]
	if !gate.TryLock() {
		return river.JobSnooze(s.options.PollInterval)
	}
	defer gate.Unlock()
	scope := s.notificationScope()
	n, err := s.store.AvatarDelivery(ctx, scope, args.DeliveryID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	attempt := avatarDeliveryAttempt(args, jobID)
	if n.State == deliverySending {
		// The job that reserved this attempt returned without persisting an outcome.
		// Its sender is no longer live under our gate; preserve uncertainty, no resend.
		if n.Version == args.Version+1 && n.AttemptID == attempt {
			return s.store.FinishAvatarDelivery(ctx, n, stateDeliveryUnknown, nil, 0)
		}
		return nil
	}
	if n.Version != args.Version || (n.State != deliveryPending && n.State != deliveryRetryable) {
		return nil
	}
	u, err := s.store.AvatarDeliveryRecipient(ctx, scope, n.ID)
	if err == nil {
		err = s.verifyNotificationUser(ctx, u)
	}
	if notificationDenied(err) {
		return s.blockAvatarDelivery(ctx, n)
	}
	if err != nil {
		return river.JobSnooze(s.options.PollInterval)
	}
	principal := db.ProfileScope{BotID: scope.BotID, Organization: scope.Organization, UserID: u.ID, Subject: u.Subject}
	buttons, err := s.avatarCropMarkup(ctx, principal, u, n.ArtifactID)
	if err != nil {
		return err
	}
	reserved, content, err := s.store.ReserveAvatarDelivery(ctx, principal, n.ID, n.Version, attempt)
	if errors.Is(err, db.ErrConflict) {
		return nil
	}
	if notificationDenied(err) || errors.Is(err, db.ErrInvalidAvatar) {
		return s.blockAvatarDelivery(ctx, n)
	}
	if err != nil {
		return err
	}
	var markup *telegram.InlineKeyboardMarkup
	if len(buttons.InlineKeyboard) > 0 {
		markup = &buttons
	}
	return s.sendDraftAvatar(ctx, reserved, content, markup)
}

func (s *Service) avatarCropMarkup(
	ctx context.Context, scope db.ProfileScope, user db.User, artifactID uuid.UUID,
) (telegram.InlineKeyboardMarkup, error) {
	if s.options.MiniAppURL == "" {
		return telegram.InlineKeyboardMarkup{}, nil
	}
	view, err := s.store.FinalizedAvatarStatus(ctx, scope, artifactID)
	if err != nil {
		return telegram.InlineKeyboardMarkup{}, err
	}
	language, err := s.notificationLanguage(ctx, user)
	if err != nil {
		return telegram.InlineKeyboardMarkup{}, err
	}
	return telegram.InlineKeyboardMarkup{InlineKeyboard: [][]telegram.InlineKeyboardButton{{{
		Text:   promptText(language, "Crop", "Кадрировать"),
		WebApp: &telegram.WebAppInfo{URL: s.options.MiniAppURL + "#avatar-crop?draft=" + view.Avatar.DraftID.String()},
	}}}}, nil
}
func (s *Service) blockAvatarDelivery(ctx context.Context, n db.AvatarDelivery) error {
	err := s.store.BlockAvatarDelivery(ctx, s.notificationScope(), n.ID, n.Version)
	if errors.Is(err, db.ErrConflict) {
		return nil
	}
	return err
}
func (s *Service) sendDraftAvatar(
	ctx context.Context, n db.AvatarDelivery, content []byte, markup *telegram.InlineKeyboardMarkup,
) error {
	receipt, sendErr := s.tg.SendDocument(
		ctx,
		telegram.SendDocumentRequest{ChatID: n.TelegramUserID, Filename: "zinusya-avatar.jpg", Content: content,
			ReplyMarkup: markup},
	)
	outcome, delay := deliveryOutcome(sendErr)
	if outcome == deliveryRetryable && delay <= 0 {
		delay = s.options.PollInterval
	}
	raw, _ := json.Marshal(map[string]any{"message_id": receipt.MessageID, "chat_id": receipt.ChatID,
		"crop_button_offered": markup != nil && sendErr == nil})
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	err := s.store.FinishAvatarDelivery(finishCtx, n, outcome, raw, delay)
	metric := metricSuccess
	if err != nil || outcome == stateDeliveryUnknown || outcome == stateDeliveryFailed {
		metric = metricError
	} else if outcome == deliveryRetryable {
		metric = metricRetry
	}
	s.record(ctx, "avatar_deliveries", metric)
	return err
}
