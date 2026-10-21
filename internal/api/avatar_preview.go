package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/avatar/frame"
	"github.com/complynx/zns-chatbot/internal/db"
)

var errAvatarPreviewFrame = errors.New("saved avatar frame unavailable")

type avatarPreviewArguments struct {
	DraftID         uuid.UUID `json:"draft_id"`
	ExpectedVersion int64     `json:"expected_version" minimum:"1"`
}
type avatarPreviewMetadata struct {
	DraftID      uuid.UUID        `json:"draft_id"`
	Version      int64            `json:"version"`
	SourceSHA256 string           `json:"source_sha256"`
	FrameSHA256  string           `json:"frame_sha256"`
	Transform    avatar.Transform `json:"transform"`
	SHA256       string           `json:"sha256"`
	SizeBytes    int64            `json:"size_bytes"`
	Width        int              `json:"width"`
	Height       int              `json:"height"`
}

func (s *Server) registerAvatarPreview(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-avatar-preview-draft",
			Method:       http.MethodPost,
			Path:         avatarPath + "preview_draft",
			MaxBodyBytes: internalBodyLimit,
		},
		s.avatarDraftPreview,
	)
	s.internal.HandleFunc("GET /internal/avatar-drafts/{id}/preview/{version}", s.avatarDraftPreviewContent)
	s.public.HandleFunc("GET /app/avatars/drafts/{id}/preview/{version}", s.miniAppAvatarPreview)
	s.public.HandleFunc("GET /app/avatars/frame/{sha}", s.avatarFrame)
}

// renderAvatarPreview returns a versioned read only. It creates no artifact,
// operation receipt, delivery or task; the original and pinned frame stay intact.
func (s *Server) renderAvatarPreview(
	ctx context.Context,
	scope db.ProfileScope,
	id uuid.UUID,
	version int64,
) (db.AvatarDraft, avatar.Rendered, error) {
	d, input, err := s.renderAvatarDraft(ctx, scope, id, version)
	return d, input.Output, err
}
func (s *Server) renderAvatarDraft(
	ctx context.Context,
	scope db.ProfileScope,
	id uuid.UUID,
	version int64,
) (db.AvatarDraft, db.AvatarDraftRender, error) {
	if id == uuid.Nil || version < 1 {
		return db.AvatarDraft{}, db.AvatarDraftRender{}, db.ErrInvalidAvatar
	}
	d, raw, err := s.readAvatarDraftOriginal(ctx, scope, id)
	if err != nil {
		return d, db.AvatarDraftRender{}, err
	}
	if d.Version != version {
		return d, db.AvatarDraftRender{}, db.ErrConflict
	}
	if d.FrameSHA256 != avatar.FrameSHA256 {
		return d, db.AvatarDraftRender{}, errAvatarPreviewFrame
	}
	if s.options.AvatarRenderer == nil {
		return d, db.AvatarDraftRender{}, avatar.ErrProcessorUnavailable
	}
	child, cancel := context.WithTimeout(ctx, avatarRenderTimeout)
	defer cancel()
	result, err := s.options.AvatarRenderer.Render(child, raw, d.Transform)
	if err == nil {
		err = child.Err()
	}
	if err != nil {
		return d, db.AvatarDraftRender{}, err
	}
	if !validAvatarPreview(d, result) {
		return d, db.AvatarDraftRender{}, avatar.ErrProcessorUnavailable
	}
	current, err := s.store.GetAvatarDraft(ctx, scope, id)
	if err != nil {
		return d, db.AvatarDraftRender{}, err
	}
	if current.Version != d.Version {
		return d, db.AvatarDraftRender{}, db.ErrConflict
	}
	return d, db.AvatarDraftRender{Original: raw, Output: result}, nil
}
func validAvatarPreview(d db.AvatarDraft, r avatar.Rendered) bool {
	if len(r.JPEG) == 0 || len(r.JPEG) > avatar.MaxOutputBytes {
		return false
	}
	digest := sha256.Sum256(r.JPEG)
	return r.Width == avatar.OutputSize && r.Height == avatar.OutputSize && r.SourceSHA256 == d.SourceSHA256 &&
		r.FrameSHA256 == d.FrameSHA256 &&
		r.MatchesTransform(d.Transform) &&
		r.SHA256 == hex.EncodeToString(digest[:]) &&
		bytes.HasPrefix(r.JPEG, []byte{0xff, 0xd8})
}
func avatarPreviewInfo(d db.AvatarDraft, r avatar.Rendered) avatarPreviewMetadata {
	return avatarPreviewMetadata{
		DraftID:      d.ID,
		Version:      d.Version,
		SourceSHA256: d.SourceSHA256,
		FrameSHA256:  d.FrameSHA256,
		Transform:    d.Transform,
		SHA256:       r.SHA256,
		SizeBytes:    int64(len(r.JPEG)),
		Width:        r.Width,
		Height:       r.Height,
	}
}
func avatarPreviewPath(d db.AvatarDraft) string {
	return "/internal/avatar-drafts/" + d.ID.String() + "/preview/" + strconv.FormatInt(d.Version, 10)
}

func (s *Server) avatarDraftPreview(
	ctx context.Context,
	in *agentReadRequest[avatarPreviewArguments],
) (*fileEnvelope, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	d, r, err := s.renderAvatarPreview(ctx, scope, in.Body.Arguments.DraftID, in.Body.Arguments.ExpectedVersion)
	if err != nil {
		return nil, avatarPreviewReadError(err)
	}
	current, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if current != scope {
		return nil, huma.Error404NotFound("avatar draft unavailable")
	}
	result := struct {
		Status      string                `json:"status"`
		OperationID string                `json:"operation_id"`
		Result      avatarPreviewMetadata `json:"result"`
		Artifacts   []fileDescriptor      `json:"artifacts"`
	}{agentSucceeded, in.Body.OperationID, avatarPreviewInfo(d, r), []fileDescriptor{{ExternalID: "avatar-preview:" + d.ID.String() + ":" + strconv.FormatInt(d.Version, 10), FetchMethod: avatarRefetchMethod, DownloadPath: avatarPreviewPath(d), MIMEType: mimeJPEG, SizeBytes: int64(len(r.JPEG)), SHA256: r.SHA256}}}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("avatar preview descriptor unavailable")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: raw}, nil
}
func avatarPreviewReadError(err error) error {
	switch {
	case errors.Is(err, errAvatarSourceUnavailable):
		return huma.Error422UnprocessableEntity(errAvatarSourceUnavailable.Error())
	case errors.Is(err, db.ErrInvalidAvatar):
		return huma.Error400BadRequest("draft ID and positive version required")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("avatar draft changed; read current state before preview")
	case errors.Is(err, errAvatarPreviewFrame):
		return huma.Error409Conflict("saved avatar frame unavailable; choose a compatible draft")
	default:
		return avatarReadError(err)
	}
}
func avatarPreviewLocator(r *http.Request) (uuid.UUID, int64, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, 0, db.ErrInvalidAvatar
	}
	version, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || version < 1 {
		return uuid.Nil, 0, db.ErrInvalidAvatar
	}
	return id, version, nil
}
func (s *Server) avatarDraftPreviewContent(w http.ResponseWriter, r *http.Request) {
	id, version, err := avatarPreviewLocator(r)
	if err != nil {
		if !writeAvatarSourceUnavailable(w, err) {
			fileHTTPError(w, avatarPreviewReadError(err))
		}
		return
	}
	auth := AuthInput{Authorization: r.Header.Get("Authorization")}
	scope, err := s.profileScope(r.Context(), auth)
	if err != nil {
		fileHTTPError(w, err)
		return
	}
	d, result, err := s.renderAvatarPreview(r.Context(), scope, id, version)
	if err != nil {
		if !writeAvatarSourceUnavailable(w, err) {
			fileHTTPError(w, avatarPreviewReadError(err))
		}
		return
	}
	current, err := s.profileScope(r.Context(), auth)
	if err != nil {
		fileHTTPError(w, err)
		return
	}
	if current != scope {
		http.NotFound(w, r)
		return
	}
	writeAvatarPreview(w, d, result)
}
func (s *Server) miniAppAvatarPreview(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	id, version, err := avatarPreviewLocator(r)
	if err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "draft ID and positive version required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), avatarUploadTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	d, result, err := s.renderAvatarPreview(ctx, miniAppOrdersScope(attempt), id, version)
	if err != nil {
		if errors.Is(err, errAvatarSourceUnavailable) {
			s.miniAppAvatarError(w, err)
			return
		}
		status := http.StatusServiceUnavailable
		if errors.Is(err, pgx.ErrNoRows) {
			status = http.StatusNotFound
		}
		if errors.Is(err, db.ErrConflict) || errors.Is(err, errAvatarPreviewFrame) {
			status = http.StatusConflict
		}
		miniAppHTTPError(w, status, "avatar preview unavailable or draft changed; read current draft")
		return
	}
	if _, err = s.authenticatedMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	writeAvatarPreview(w, d, result)
}
func writeAvatarPreview(w http.ResponseWriter, d db.AvatarDraft, r avatar.Rendered) {
	w.Header().Set("Content-Type", mimeJPEG)
	w.Header().Set("Content-Disposition", `attachment; filename="avatar-preview.jpg"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Zinusya-Draft-Version", strconv.FormatInt(d.Version, 10))
	_, _ = w.Write(r.JPEG) // #nosec G705 -- owner-authorized validated JPEG with attachment, nosniff and sandbox CSP.
}

// avatarFrame serves only the public immutable brand asset, never user content.
func (s *Server) avatarFrame(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("sha") != avatar.FrameSHA256 || frame.SHA256() != avatar.FrameSHA256 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", mimePNG)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+avatar.FrameSHA256+`"`)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "avatar-frame.png", time.Time{}, frame.Reader())
}
