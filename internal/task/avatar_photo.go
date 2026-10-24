package task

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

var errNativeAvatarSource = errors.New("native avatar source unavailable")

const nativeAvatarRenderTimeout = 3 * time.Second

// Native photo work uses durable draft/finalization receipts, never a Loom run.
func (s *Service) nativeAvatarPhoto(ctx context.Context, t *db.Task, c checkpoint, i intent) error {
	user, _, err := s.authorize(ctx, *t, i)
	if err == nil {
		_, err = s.taskPrincipal(ctx, user)
	}
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	if !nativeAvatarImage(i.Attachments) {
		return s.finish(ctx, t, c, i, stateFailed, kindUnsupported)
	}
	scope := db.ProfileScope{BotID: s.options.BotID, Organization: user.Organization,
		Subject: user.Subject, UserID: user.ID}
	receipt, err := s.nativeAvatarResult(ctx, scope, t.ID, i.Attachments[0].ID)
	if err != nil {
		if errors.Is(err, errNativeAvatarSource) || errors.Is(err, avatar.ErrInvalidImage) ||
			errors.Is(
				err,
				avatar.ErrImageLimit,
			) || errors.Is(err, db.ErrAvatarQuota) || errors.Is(err, db.ErrConflict) {
			text := promptText(s.languageFor(ctx, i),
				"I could not make the framed avatar. Open /avatar to check the photo or upload another original.",
				"Не смогла сделать аватарку в рамке. Проверь фото или загрузи другой оригинал через /avatar.")
			t.State = stateFailed
			if saveErr := s.save(ctx, t, c, s.result(i, text, "avatar_photo_failed")); saveErr != nil {
				return s.wait()
			}
			return s.deliver(ctx, t, c)
		}
		s.reportDatabaseFailure(ctx, t.ID, "native_avatar", err)
		return s.wait()
	}
	c.NativeAvatar = &receipt
	t.State = stateCompleted
	if err = s.save(ctx, t, c, nil); err != nil {
		return s.wait()
	}
	return nil
}

func (s *Service) nativeAvatarResult(
	ctx context.Context, scope db.ProfileScope, taskID, sourceID uuid.UUID,
) (db.AvatarDraftReceipt, error) {
	finalizeID := uuid.NewSHA1(taskID, []byte("native-avatar-finalize"))
	receipt, err := s.store.AvatarDraftFinalizationOperation(ctx, scope, finalizeID)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return receipt, err
	}
	create := db.AvatarDraftCreate{
		OperationID:  uuid.NewSHA1(taskID, []byte("native-avatar-draft")),
		SourceFileID: sourceID,
	}
	draft, err := s.store.AvatarDraftPreflight(ctx, scope, create)
	if err != nil {
		return receipt, err
	}
	var raw []byte
	var rendered avatar.Rendered
	if draft == nil {
		raw, rendered, err = s.renderNativeAvatar(ctx, scope, sourceID)
		if err != nil {
			return receipt, err
		}
		created, createErr := s.store.CreateSystemAvatarDraft(ctx, scope, create, raw, rendered)
		if createErr != nil {
			return receipt, createErr
		}
		draft = &created
	}
	finalize := db.AvatarDraftFinalize{OperationID: finalizeID, DraftID: draft.ID, ExpectedVersion: draft.Version}
	prior, err := s.store.AvatarDraftFinalizePreflight(ctx, scope, finalize)
	if err != nil {
		return receipt, err
	}
	if prior != nil {
		return *prior, nil
	}
	if len(raw) == 0 {
		raw, rendered, err = s.renderNativeAvatar(ctx, scope, sourceID)
		if err != nil {
			return receipt, err
		}
	}
	return s.store.CommitSystemAvatarDraft(ctx, scope, finalize, db.AvatarDraftRender{Original: raw, Output: rendered})
}

func (s *Service) renderNativeAvatar(
	ctx context.Context, scope db.ProfileScope, sourceID uuid.UUID,
) ([]byte, avatar.Rendered, error) {
	var output avatar.Rendered
	owner := db.EventContextScope{UserID: scope.UserID, Subject: scope.Subject}
	owner.BotID, owner.Organization = scope.BotID, scope.Organization
	record, err := s.store.File(ctx, owner, sourceID)
	if err != nil {
		return nil, output, err
	}
	if record.SizeBytes > files.MaxReadBytes {
		return nil, output, avatar.ErrImageLimit
	}
	if s.options.AvatarRenderer == nil {
		return nil, output, avatar.ErrProcessorUnavailable
	}
	raw, err := s.tg.ReadFile(ctx, record.TelegramID, files.MaxReadBytes)
	if errors.Is(err, telegram.ErrFileTooLarge) {
		return nil, output, avatar.ErrImageLimit
	}
	if errors.Is(err, telegram.ErrFileNotFound) || errors.Is(err, telegram.ErrFileAccessDenied) {
		return nil, output, errNativeAvatarSource
	}
	if err != nil {
		return nil, output, err
	}
	child, cancel := context.WithTimeout(ctx, nativeAvatarRenderTimeout)
	defer cancel()
	output, err = s.options.AvatarRenderer.Render(child, raw, avatar.IdentityTransform())
	if err == nil {
		err = child.Err()
	}
	return raw, output, err
}
