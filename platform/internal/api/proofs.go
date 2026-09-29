package api

import (
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func proofRoutes(mux *http.ServeMux, service orders.Service, logger *slog.Logger) {
	mux.HandleFunc("POST /v1/order-proofs", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, orders.MaxProofBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			JSON(w, http.StatusRequestEntityTooLarge, map[string]string{codeField: "invalid_proof"})
			return
		}
		proof, err := service.UploadProof(r.Context(), requestOwner(r), r.URL.Query().Get("filename"), body)
		respond(logger, w, proof, err)
	})
	mux.HandleFunc("GET /v1/order-events/{event}/orders/{order}/proof", func(w http.ResponseWriter, r *http.Request) {
		proof, err := service.OrderProof(r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("order"))
		respond(logger, w, proof, err)
	})
	mux.HandleFunc(
		"GET /v1/order-events/{event}/orders/{order}/proof/file",
		func(w http.ResponseWriter, r *http.Request) {
			proof, err := service.OrderProof(r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("order"))
			if err != nil {
				respond(logger, w, nil, err)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().
				Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": proof.Filename}))
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Order-Version", strconv.FormatInt(proof.Version, 10))
			w.Header().Set("X-Payment-Attempt", proof.Attempt)
			w.Header().Set("X-Proof-Id", proof.ID)
			_, _ = w.Write(proof.Body)
		},
	)
}
