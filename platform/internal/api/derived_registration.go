package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func derivedRegistrationRoutes(
	mux *http.ServeMux,
	service derivedmutation.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	passOperationRoutes(mux, service, authorizer, signer, logger)
	mux.Handle("POST /internal/registration/admission", authenticatedDerivation(authorizer, signer,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request passbooking.AdmissionRequest
			if Decode(w, r, &request) != nil || request.Ingress == nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.Registration.CaptureAdmission(r.Context(), requestOwner(r), request)
			respondPassProfile(logger, w, value, err)
		}), logger))
	mux.Handle("POST /internal/derived/pass-admission", authenticatedDerivation(authorizer, signer,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var request passbooking.AdmissionRequest
			source, err := decodeDerivedMutation(w, r, &request, maxRequestBytes)
			if err != nil || request.Ingress == nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.CapturePassAdmission(r.Context(), requestOwner(r), request, source)
			respondPassProfile(logger, w, value, err)
		}), logger))
	mux.Handle(
		"POST /internal/derived/pass-actions",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passbooking.Command
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.ExecutePassBooking(r.Context(), requestOwner(r), command, source)
			respondPassProfile(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/pass-assignments",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passbooking.AdminAssignment
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.AssignPass(r.Context(), requestOwner(r), command, source)
			respond(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/pass-profiles",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passes.Command
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.ExecutePassProfile(r.Context(), requestOwner(r), command, source)
			respondPassProfile(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/pass-batches",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passbooking.RuntimeBatch
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.RunPassBatch(r.Context(), requestOwner(r), command, source)
			respond(logger, w, value, err)
		}), logger),
	)
}
