package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func derivedPaymentReceiptRoute(
	mux *http.ServeMux,
	service derivedmutation.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.Handle(
		"POST /internal/derived/pass-payments/receipt",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passbooking.Command
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.PassPaymentReceipt(r.Context(), requestOwner(r), command, source)
			respond(logger, w, result, err)
		}), logger),
	)
}
