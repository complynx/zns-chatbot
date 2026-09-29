package api

import (
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const paymentFilenameField = "filename"

func passPaymentRoutes(mux *http.ServeMux, service passbooking.Service, files orders.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/passes/events/{event}/me/payment-quote", func(w http.ResponseWriter, r *http.Request) {
		quote, err := service.PaymentQuote(r.Context(), requestOwner(r), r.PathValue("event"))
		respond(logger, w, quote, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/payment-queue", func(w http.ResponseWriter, r *http.Request) {
		page, err := service.PaymentQueue(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.URL.Query().Get("after"),
		)
		respond(logger, w, page, err)
	})
	// Both domains use the existing immutable, owner-bound file store.
	mux.HandleFunc("POST /v1/pass-proofs", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, orders.MaxProofBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			JSON(w, http.StatusRequestEntityTooLarge, map[string]string{codeField: "invalid_proof"})
			return
		}
		proof, err := files.UploadProof(r.Context(), requestOwner(r), r.URL.Query().Get(paymentFilenameField), body)
		respond(logger, w, proof, err)
	})
	mux.HandleFunc(
		"GET /v1/passes/events/{event}/participants/{owner}/payment",
		func(w http.ResponseWriter, r *http.Request) {
			payment, err := service.Payment(r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("owner"))
			respond(logger, w, payment, err)
		},
	)
	mux.HandleFunc(
		"GET /v1/passes/events/{event}/participants/{owner}/payment/file",
		func(w http.ResponseWriter, r *http.Request) {
			proof, err := service.PaymentProof(r.Context(), requestOwner(r), r.PathValue("event"), r.PathValue("owner"))
			if err != nil {
				respond(logger, w, nil, err)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().
				Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{paymentFilenameField: proof.Filename}))
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Pass-Version", strconv.FormatInt(proof.Version, 10))
			w.Header().Set("X-Payment-Attempt", proof.Attempt)
			_, _ = w.Write(proof.Body)
		},
	)
}
