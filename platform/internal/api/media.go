package api

import (
	"io"
	"log/slog"
	"mime"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/media"
)

func mediaRoutes(mux *http.ServeMux, service media.Service, logger *slog.Logger) {
	mux.HandleFunc("POST /v1/media", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, media.MaxMediaBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			JSON(w, http.StatusRequestEntityTooLarge, map[string]string{codeField: "invalid_media"})
			return
		}
		attachment, err := service.Upload(r.Context(), requestOwner(r), r.URL.Query().Get("filename"), body)
		respond(logger, w, attachment, err)
	})
	mux.HandleFunc("GET /v1/media/{id}", func(w http.ResponseWriter, r *http.Request) {
		attachment, err := service.Get(r.Context(), requestOwner(r), r.PathValue("id"))
		respond(logger, w, attachment, err)
	})
	mux.HandleFunc("GET /v1/media/{id}/file", func(w http.ResponseWriter, r *http.Request) {
		attachment, err := service.Get(r.Context(), requestOwner(r), r.PathValue("id"))
		if err != nil {
			respond(logger, w, nil, err)
			return
		}
		w.Header().Set("Content-Type", attachment.MIME)
		w.Header().
			Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename}))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(attachment.Body)
	})
	mux.HandleFunc("POST /v1/media/{id}/proof", func(w http.ResponseWriter, r *http.Request) {
		proof, err := service.PromoteProof(r.Context(), requestOwner(r), r.PathValue("id"))
		respond(logger, w, proof, err)
	})
}
