package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
)

const avatarUploadOperationHeader = "X-Zinusya-Operation-Id"

type miniAppAvatarImport struct {
	OperationID  uuid.UUID `json:"operation_id"`
	SourceFileID uuid.UUID `json:"source_file_id"`
}
type miniAppAvatarSave struct {
	OperationID     uuid.UUID        `json:"operation_id"`
	ExpectedVersion int64            `json:"expected_version"`
	Transform       avatar.Transform `json:"transform"`
}

func (s *Server) registerMiniAppAvatars() {
	s.public.HandleFunc("GET /app/avatars/drafts", s.miniAppAvatarRead)
	s.public.HandleFunc("GET /app/avatars/drafts/{id}", s.miniAppAvatarRead)
	s.public.HandleFunc("GET /app/avatars/drafts/{id}/original", s.miniAppAvatarOriginal)
	s.public.HandleFunc("GET /app/avatars/operations/{operation}", s.miniAppAvatarRead)
	s.public.HandleFunc("POST /app/avatars/drafts", s.miniAppAvatarImport)
	s.public.HandleFunc("POST /app/avatars/drafts/upload", s.miniAppAvatarUpload)
	s.public.HandleFunc("POST /app/avatars/drafts/{id}", s.miniAppAvatarSave)
}
func decodeMiniAppAvatar(w http.ResponseWriter, r *http.Request, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar draft request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar draft request")
		return false
	}
	return true
}
func (s *Server) miniAppAvatarRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	scope := miniAppOrdersScope(attempt)
	var result any
	switch {
	case r.PathValue("operation") != "":
		id, parseErr := uuid.Parse(r.PathValue("operation"))
		if parseErr != nil || id == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar operation")
			return
		}
		result, err = s.store.AvatarDraftOperation(ctx, scope, id)
	case r.PathValue("id") != "":
		id, parseErr := uuid.Parse(r.PathValue("id"))
		if parseErr != nil || id == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar draft")
			return
		}
		result, err = s.store.GetAvatarDraft(ctx, scope, id)
	default:
		var drafts []db.AvatarDraft
		drafts, err = s.store.ListAvatarDrafts(ctx, scope)
		result = avatarDraftList{drafts}
	}
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	s.writeMiniAppAvatar(ctx, w, r, result)
}
func (s *Server) writeMiniAppAvatar(ctx context.Context, w http.ResponseWriter, r *http.Request, result any) {
	if _, err := s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentTypeJSON)
	_ = json.NewEncoder(w).Encode(result)
}
func (s *Server) miniAppAvatarOriginal(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || id == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar draft")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	d, raw, err := s.store.AvatarDraftOriginal(ctx, miniAppOrdersScope(attempt), id)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	writeAvatarOriginal(w, d, raw)
}
func (s *Server) miniAppAvatarImport(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	var input miniAppAvatarImport
	if !decodeMiniAppAvatar(w, r, &input) {
		return
	}
	if input.OperationID == uuid.Nil || input.SourceFileID == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "operation and owned source file required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), avatarUploadTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	a := db.AvatarDraftCreate{OperationID: input.OperationID, SourceFileID: input.SourceFileID}
	scope := miniAppOrdersScope(attempt)
	prior, err := s.store.AvatarDraftPreflight(ctx, scope, a)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	var raw []byte
	var proof avatar.Rendered
	if prior == nil {
		record, fileErr := s.store.File(
			ctx,
			db.EventContextScope{
				BotID:        scope.BotID,
				Organization: scope.Organization,
				Subject:      scope.Subject,
				UserID:       scope.UserID,
			},
			a.SourceFileID,
		)
		if fileErr != nil {
			s.miniAppAvatarError(w, fileErr)
			return
		}
		raw, err = s.readFile(ctx, record)
		if err != nil {
			s.miniAppAvatarError(w, err)
			return
		}
		proof, err = s.validateAvatarOriginal(ctx, raw)
		if err != nil {
			s.miniAppAvatarError(w, err)
			return
		}
	}
	s.commitMiniAppAvatar(ctx, w, r, scope, a, raw, proof)
}
func (s *Server) miniAppAvatarUpload(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppOriginRequest(w, r) {
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || (media != "application/octet-stream" && media != mimeJPEG && media != mimePNG) {
		miniAppHTTPError(w, http.StatusUnsupportedMediaType, "static image upload required")
		return
	}
	operation, err := uuid.Parse(r.Header.Get(avatarUploadOperationHeader))
	if err != nil || operation == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "operation header must be a nonzero UUID")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), avatarUploadTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, avatar.MaxInputBytes))
	if err != nil {
		miniAppHTTPError(w, http.StatusRequestEntityTooLarge, "avatar upload exceeds image limit or is incomplete")
		return
	}
	digest := sha256.Sum256(raw)
	a := db.AvatarDraftCreate{OperationID: operation, UploadSHA256: hex.EncodeToString(digest[:])}
	scope := miniAppOrdersScope(attempt)
	prior, err := s.store.AvatarDraftPreflight(ctx, scope, a)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	var proof avatar.Rendered
	if prior == nil {
		proof, err = s.validateAvatarOriginal(ctx, raw)
		if err != nil {
			s.miniAppAvatarError(w, err)
			return
		}
	}
	s.commitMiniAppAvatar(ctx, w, r, scope, a, raw, proof)
}

func (s *Server) commitMiniAppAvatar(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	scope db.ProfileScope,
	a db.AvatarDraftCreate,
	raw []byte,
	proof avatar.Rendered,
) {
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.CreateMiniAppAvatarDraft(ctx, scope, bearer, s.miniAppKey.Generation(), a, raw, proof)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	s.writeMiniAppAvatar(ctx, w, r, result)
}
func (s *Server) miniAppAvatarSave(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || id == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar draft")
		return
	}
	var input miniAppAvatarSave
	if !decodeMiniAppAvatar(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.SaveMiniAppAvatarDraft(
		ctx,
		miniAppOrdersScope(attempt),
		bearer,
		s.miniAppKey.Generation(),
		db.AvatarDraftSave{
			OperationID:     input.OperationID,
			DraftID:         id,
			ExpectedVersion: input.ExpectedVersion,
			Transform:       input.Transform,
		},
	)
	if err != nil {
		s.miniAppAvatarError(w, err)
		return
	}
	s.writeMiniAppAvatar(ctx, w, r, result)
}
func (s *Server) miniAppAvatarError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		miniAppHTTPError(w, http.StatusNotFound, "avatar source, draft or operation unavailable")
	case errors.Is(err, db.ErrConflict):
		miniAppHTTPError(
			w,
			http.StatusConflict,
			"avatar draft changed or operation arguments conflict; read current draft",
		)
	case errors.Is(err, db.ErrAvatarQuota):
		miniAppHTTPError(w, http.StatusConflict, "generated avatar storage quota reached")
	case errors.Is(err, db.ErrAvatarDraftQuota):
		miniAppHTTPError(
			w,
			http.StatusConflict,
			"avatar originals quota reached: at most five originals and 50 MiB per owner",
		)
	case errors.Is(err, avatar.ErrImageLimit):
		miniAppHTTPError(w, http.StatusRequestEntityTooLarge, "avatar image exceeds processing limits")
	case errors.Is(err, avatar.ErrInvalidImage),
		errors.Is(err, avatar.ErrInvalidTransform),
		errors.Is(err, db.ErrInvalidAvatar):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid avatar image, operation or transform")
	default:
		if status, ok := errors.AsType[huma.StatusError](err); ok {
			miniAppHTTPError(w, status.GetStatus(), "avatar source unavailable")
			return
		}
		miniAppError(w, err)
	}
}
