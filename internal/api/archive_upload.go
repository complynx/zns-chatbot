package api

import (
	"context"
	"io"
	"mime"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func (s *Server) validateArchiveUpload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), archiveValidationTimeout)
	defer cancel()
	auth := r.Header.Get("Authorization")
	if _, err := s.authorize(ctx, auth, true); err != nil {
		fileHTTPError(w, err)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/gzip" || len(params) != 0 {
		http.Error(w, "application/gzip required", http.StatusUnsupportedMediaType)
		return
	}
	if err = s.beginArchiveValidation(); err != nil {
		fileHTTPError(w, err)
		return
	}
	defer s.endArchiveValidation()
	body := http.MaxBytesReader(w, r.Body, legacyimport.MaxBundleBytes)
	defer func() { _ = body.Close() }()
	result, err := s.validateArchive(ctx, body, r.Header.Get("X-Content-Sha256"))
	if err != nil {
		fileHTTPError(w, err)
		return
	}
	if _, err = s.authorize(ctx, auth, true); err != nil {
		fileHTTPError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) validateArchive(
	ctx context.Context,
	input io.Reader,
	digest string,
) (legacyimport.BundleValidation, error) {
	result, err := legacyimport.ValidateBundle(ctx, input, digest)
	if err != nil {
		return legacyimport.BundleValidation{}, huma.Error400BadRequest("archive validation failed")
	}
	if result.SourceBotID != s.options.BotID {
		return legacyimport.BundleValidation{}, huma.Error409Conflict("archive belongs to another bot")
	}
	return result, nil
}
