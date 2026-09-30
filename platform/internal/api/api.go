package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/workflow"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

type ownerKey struct{}

const maxRequestBytes = 65536
const unauthorized = "unauthorized"

func requestOwner(r *http.Request) string {
	principal, _ := r.Context().Value(ownerKey{}).(applicationauth.Principal)
	return principal.Owner()
}

func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBytes)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	var tail any
	if e := d.Decode(&tail); !errors.Is(e, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}
func Handler(deps appservices.Services, signer identity.Signer, logger *slog.Logger) http.Handler {
	return AuthenticatedHandler(deps, signer, logger, func(_ context.Context, token string) (string, error) {
		return signer.Verify(token)
	})
}

// AuthenticatedHandler retains separate service authentication for notifications.
func AuthenticatedHandler(
	deps appservices.Services,
	signer identity.Signer,
	logger *slog.Logger,
	verify VerifyOwner,
) http.Handler {
	s := deps.Core
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := s.DB.Ping(r.Context()); e != nil {
			JSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	business := http.NewServeMux()
	adminUtilityRoutes(business, deps.AdminUtilities, logger)
	privilegedReadRoutes(business, s, deps.Registration, deps.Massage, logger)
	modelSettingsRoutes(business, deps.ModelSettings, logger)
	creditsRoutes(business, deps.Credits, logger)
	adminMessageRoutes(business, mux, deps.AdminMessages, signer, logger)
	historyRoutes(business, deps.Conversation, logger)
	mediaRoutes(business, deps.Media, logger)
	knowledgeRoutes(business, deps.Knowledge, logger)
	memoryRoutes(business, deps.Knowledge, logger)
	memoryProvenanceRoutes(mux, deps.Knowledge, deps.Conversation, signer, logger)
	telegramMetadataRoutes(mux, deps.Account, signer, logger)
	browserAuthRoutes(mux, s, signer, logger)
	massageRoutes(business, deps.Massage, logger)
	massageNavigationRoutes(business, deps.Massage, logger)
	preferenceRoutes(business, deps.Account, logger)
	passProfileRoutes(business, deps.PassProfiles, logger)
	passBookingRoutes(business, deps.Registration, deps.Orders, deps.DerivedMutations, logger)
	passNavigationRoutes(business, deps.Registration, logger)
	notificationRoutes(mux, deps.Orders, deps.Massage, deps.Registration, signer, logger)
	orderRoutes(business, deps.Orders, logger)
	legacyOrderRoutes(business, deps.LegacyOrders, logger)
	legacyFoodRoutes(business, mux, deps.LegacyFood, signer, logger)
	business.HandleFunc("GET /v1/business-capabilities", func(w http.ResponseWriter, r *http.Request) {
		v, e := s.Capabilities(r.Context(), requestOwner(r), r.URL.Query().Get("event"))
		respond(logger, w, v, e)
	})
	business.HandleFunc(
		"GET /v1/catalog",
		func(w http.ResponseWriter, r *http.Request) {
			v, e := deps.Workflow.Catalog(r.Context())
			respond(logger, w, v, e)
		},
	)
	business.HandleFunc("GET /v1/workflow", func(w http.ResponseWriter, r *http.Request) {
		v, e := deps.Workflow.Current(r.Context(), requestOwner(r))
		respond(logger, w, v, e)
	})
	business.HandleFunc("POST /v1/actions", func(w http.ResponseWriter, r *http.Request) {
		var a workflow.Action
		if Decode(w, r, &a) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		v, e := deps.Workflow.Execute(r.Context(), requestOwner(r), a)
		respond(logger, w, v, e)
	})
	authorizer := applicationauth.Authorizer{DB: s.DB, Verify: applicationauth.VerifyOwner(verify)}
	botDeliveryRoutes(mux, deps.BotDelivery, authorizer, signer, logger)
	memoryReadStateRoutes(mux, deps.MemoryReadState, authorizer, signer, logger)
	derivedMutationRoutes(mux, deps.DerivedMutations, authorizer, signer, logger)
	derivedReceiptRoutes(mux, deps.DerivedMutations, deps.Knowledge, authorizer, signer, logger)
	derivedSettingsRoutes(mux, deps.DerivedMutations, authorizer, signer, logger)
	derivedBusinessRoutes(mux, deps.DerivedMutations, authorizer, signer, logger)
	derivedRegistrationRoutes(mux, deps.DerivedMutations, authorizer, signer, logger)
	derivedKnowledgeRoutes(mux, deps.Knowledge, authorizer, signer, logger)
	knowledgeSubmissionRoutes(mux, deps.Knowledge, authorizer, signer, logger)
	derivedBroadcastRoutes(mux, deps.AdminMessages, authorizer, signer, logger)
	passExportAuthorityRoutes(mux, deps.Registration, authorizer, signer, logger)
	mux.Handle("/v1/", authenticated(authorizer, business, logger))
	return mux
}

func authenticated(authorizer applicationauth.Authorizer, business http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !bearer {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		principal, e := authorizer.Authorize(r.Context(), token)
		if errors.Is(e, applicationauth.ErrUnauthorized) {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		if errors.Is(e, applicationauth.ErrForbidden) {
			JSON(w, http.StatusForbidden, map[string]string{codeField: "forbidden"})
			return
		}
		if e != nil {
			respond(logger, w, nil, e)
			return
		}
		business.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ownerKey{}, principal)))
	})
}
func respond(logger *slog.Logger, w http.ResponseWriter, v any, e error) {
	if e == nil {
		JSON(w, http.StatusOK, v)
		return
	}
	databaseFailure := markDatabaseFailure(w, e)
	if p, ok := errors.AsType[*core.ProblemError](e); ok {
		JSON(w, p.Status, p)
		return
	}
	if databaseFailure {
		logger.Error("API database request failed")
	} else {
		logger.Error("API request failed", "error", e)
	}
	JSON(w, http.StatusInternalServerError, map[string]string{codeField: internalError})
}

const codeField = "code"
const invalidJSON = "invalid_json"
