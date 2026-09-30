package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func derivedReceiptRoutes(
	mux *http.ServeMux,
	service derivedmutation.Service,
	knowledgeService knowledge.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	derivedPaymentReceiptRoute(mux, service, authorizer, signer, logger)
	mux.Handle(
		"POST /internal/derived/order-actions/receipt",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command orders.Command
			if _, err := decodeDerivedMutation(w, r, &command, maxDerivedOrderCommandBytes); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.OrderReceipt(r.Context(), requestOwner(r), command)
			respond(logger, w, result, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/actions/receipt",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command workflow.Action
			if _, err := decodeDerivedMutation(w, r, &command, maxRequestBytes); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.WorkflowReceipt(r.Context(), requestOwner(r), command)
			respond(logger, w, result, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/pass-actions/receipt",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passbooking.Command
			if _, err := decodeDerivedMutation(w, r, &command, maxRequestBytes); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.PassBookingReceipt(r.Context(), requestOwner(r), command)
			respond(logger, w, result, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/pass-assignments/receipt",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passbooking.AdminAssignment
			if _, err := decodeDerivedMutation(w, r, &command, maxRequestBytes); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.PassAssignmentReceipt(r.Context(), requestOwner(r), command)
			respond(logger, w, result, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/pass-profiles/receipt",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command passes.Command
			if _, err := decodeDerivedMutation(w, r, &command, maxRequestBytes); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.PassProfileReceipt(r.Context(), requestOwner(r), command)
			respond(logger, w, result, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/knowledge/derived/receipt",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command knowledge.Command
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, found, err := knowledgeService.CommandReceipt(r.Context(), requestOwner(r), command, source)
			respond(logger, w, derivedmutation.Receipt[knowledge.Result]{Found: found, Result: value}, err)
		}), logger),
	)
}
