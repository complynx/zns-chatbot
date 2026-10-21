package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

var errAvatarSourceUnavailable = errors.New("avatar source unavailable; send the photo again")

// Callers authorize before resolving and again before returning content or
// committing. Each store call releases its connection before Telegram IO.
func (s *Server) readAvatarDraftOriginal(
	ctx context.Context,
	scope db.ProfileScope,
	id uuid.UUID,
) (db.AvatarDraft, []byte, error) {
	input, err := s.avatarDraftInput(ctx, scope, id)
	if err != nil {
		return db.AvatarDraft{}, nil, err
	}
	raw := input.Original
	if input.TelegramID != "" {
		raw, err = s.readFile(ctx, db.File{TelegramID: input.TelegramID})
		if err != nil {
			if ctx.Err() != nil {
				return input.Draft, nil, ctx.Err()
			}
			return input.Draft, nil, errAvatarSourceUnavailable
		}
	}
	digest := sha256.Sum256(raw)
	if int64(len(raw)) != input.Draft.SizeBytes || hex.EncodeToString(digest[:]) != input.Draft.SourceSHA256 ||
		http.DetectContentType(raw) != input.Draft.MIMEType {
		return input.Draft, nil, db.ErrConflict
	}
	current, err := s.avatarDraftInput(ctx, scope, id)
	if err != nil {
		return input.Draft, nil, err
	}
	if current.Draft.Version != input.Draft.Version || current.TelegramID != input.TelegramID ||
		current.Draft.SourceSHA256 != input.Draft.SourceSHA256 {
		return input.Draft, nil, db.ErrConflict
	}
	return input.Draft, raw, nil
}

func (s *Server) avatarDraftInput(
	ctx context.Context,
	scope db.ProfileScope,
	id uuid.UUID,
) (db.AvatarDraftInput, error) {
	draft, err := s.store.GetAvatarDraft(ctx, scope, id)
	if err != nil {
		return db.AvatarDraftInput{}, err
	}
	child, release, err := s.avatarHistoryValidation(ctx, draft.HistorySource != nil)
	if err != nil {
		return db.AvatarDraftInput{}, err
	}
	defer release()
	return s.store.AvatarDraftSource(child, scope, id)
}

func (s *Server) readAvatarCreationSource(
	ctx context.Context,
	scope db.ProfileScope,
	a db.AvatarDraftCreate,
) ([]byte, error) {
	var record db.File
	if a.HistorySource != nil {
		child, release, err := s.avatarHistoryValidation(ctx, true)
		if err != nil {
			return nil, err
		}
		// Native session/IAM checks may use this pool. They run after processing and
		// before the atomic write, not inside this archive reader's transaction.
		source, err := s.store.OwnerHistoryAvatarSource(
			child,
			scope,
			a.HistorySource.RecordID,
			a.HistorySource.ArchiveID,
			func(context.Context) error { return nil },
		)
		release()
		if err != nil {
			return nil, historyError(err)
		}
		record = db.File{TelegramID: source.FileID}
	} else {
		source, err := s.store.File(ctx, db.EventContextScope{
			BotID: scope.BotID, Organization: scope.Organization, Subject: scope.Subject, UserID: scope.UserID,
		}, a.SourceFileID)
		if err != nil {
			return nil, avatarReadError(err)
		}
		record = source
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var status huma.StatusError
		if errors.As(err, &status) && status.GetStatus() == http.StatusRequestEntityTooLarge {
			return nil, err
		}
		return nil, errAvatarSourceUnavailable
	}
	return raw, nil
}

func writeAvatarSourceUnavailable(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, errAvatarSourceUnavailable) {
		return false
	}
	w.Header().Set("Cache-Control", "private, no-store")
	http.Error(w, "source photo unavailable; send it again or upload another original", http.StatusUnprocessableEntity)
	return true
}

// Slots cover archive reads and their atomic revalidation, never Telegram IO or rendering.
func (s *Server) avatarHistoryValidation(ctx context.Context, historical bool) (context.Context, func(), error) {
	if !historical {
		return ctx, func() {}, nil
	}
	if err := s.beginArchiveValidation(); err != nil {
		return nil, nil, err
	}
	child, cancel := context.WithTimeout(ctx, archiveValidationTimeout)
	return child, func() {
		cancel()
		s.endArchiveValidation()
	}, nil
}

func (s *Server) avatarCreationCommitContext(ctx context.Context, scope db.ProfileScope,
	a db.AvatarDraftCreate,
) (context.Context, func(), error) {
	if a.HistorySource == nil {
		return s.avatarHistoryValidation(ctx, false)
	}
	prior, err := s.store.AvatarDraftPreflight(ctx, scope, a)
	if err != nil {
		return nil, nil, err
	}
	return s.avatarHistoryValidation(ctx, prior == nil)
}

func (s *Server) avatarFinalizationCommitContext(ctx context.Context, scope db.ProfileScope,
	a db.AvatarDraftFinalize,
) (context.Context, func(), error) {
	prior, err := s.store.AvatarDraftFinalizePreflight(ctx, scope, a)
	if err != nil {
		return nil, nil, err
	}
	if prior != nil {
		return s.avatarHistoryValidation(ctx, false)
	}
	draft, err := s.store.GetAvatarDraft(ctx, scope, a.DraftID)
	if err != nil {
		return nil, nil, err
	}
	return s.avatarHistoryValidation(ctx, draft.HistorySource != nil)
}
