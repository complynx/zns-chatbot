package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

type miniAppOrdersQuoteInput struct {
	ExpectedEventVersion   *int64             `json:"expected_event_version"`
	ExpectedCatalogVersion *int64             `json:"expected_catalog_version"`
	Selections             []orders.Selection `json:"selections"`
}

type miniAppOrdersSelectionInput struct {
	miniAppOrdersQuoteInput

	OperationID          uuid.UUID `json:"operation_id"`
	ExpectedDraftVersion *int64    `json:"expected_draft_version"`
}

func (s *Server) registerMiniAppOrders() {
	s.registerMiniAppOrderSubmission()
	s.public.HandleFunc("GET /app/orders/events", s.miniAppOrdersEvents)
	s.public.HandleFunc("GET /app/orders/{event}/catalog", s.miniAppOrdersRead)
	s.public.HandleFunc("GET /app/orders/{event}/selection", s.miniAppOrdersRead)
	s.public.HandleFunc("GET /app/orders/{event}/selection/operations/{operation}", s.miniAppOrdersRead)
	s.public.HandleFunc("POST /app/orders/{event}/selection", s.miniAppOrdersSave)
	s.public.HandleFunc("POST /app/orders/{event}/quote", s.miniAppOrdersQuote)
}

func miniAppOrdersScope(attempt db.MiniAppAttempt) db.ProfileScope {
	u := attempt.User
	return db.ProfileScope{BotID: u.BotID, Organization: u.Organization, Subject: u.Subject, UserID: u.ID}
}

func (s *Server) miniAppOrdersEvents(w http.ResponseWriter, r *http.Request) {
	s.miniAppEventList(w, r, s.store.ListOwnOrdersSelectionEvents, s.miniAppOrdersError)
}

func (s *Server) miniAppOrdersRead(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid orders event")
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
		operation, parseErr := uuid.Parse(r.PathValue("operation"))
		if parseErr != nil || operation == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid selection operation")
			return
		}
		result, err = s.store.OwnOrdersSelectionOperation(ctx, scope, event, operation)
	case strings.HasSuffix(r.URL.Path, "/catalog"):
		result, err = s.store.OwnOrdersCatalog(ctx, scope, event)
	default:
		result, err = s.store.OwnOrdersSelection(ctx, scope, event)
	}
	if err != nil {
		s.miniAppOrdersError(ctx, w, r, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

// decodeMiniAppOrders requires one bounded JSON object and rejects caller prices,
// owner IDs, unknown fields and trailing content before any business write.
func decodeMiniAppOrders(w http.ResponseWriter, r *http.Request, input any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, miniAppBodyLimit))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid orders request")
		return false
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid orders request")
		return false
	}
	return true
}

func validMiniAppOrdersQuote(input miniAppOrdersQuoteInput) bool {
	return input.ExpectedEventVersion != nil && *input.ExpectedEventVersion > 0 &&
		input.ExpectedCatalogVersion != nil && *input.ExpectedCatalogVersion > 0 && input.Selections != nil
}

func (s *Server) miniAppOrdersSave(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid orders event")
		return
	}
	var input miniAppOrdersSelectionInput
	if !decodeMiniAppOrders(w, r, &input) {
		return
	}
	if !validMiniAppOrdersQuote(input.miniAppOrdersQuoteInput) || input.ExpectedDraftVersion == nil ||
		*input.ExpectedDraftVersion < 0 ||
		input.OperationID == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "explicit operation, versions and selections required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	if _, err = s.authenticatedMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.SaveMiniAppOrdersSelection(
		ctx,
		db.EventScope{BotID: s.options.BotID, Organization: s.options.OrganizationID},
		bearer,
		s.miniAppKey.Generation(),
		db.OrdersSelectionChange{
			OperationID:            input.OperationID,
			EventID:                event,
			ExpectedEventVersion:   *input.ExpectedEventVersion,
			ExpectedCatalogVersion: *input.ExpectedCatalogVersion,
			ExpectedDraftVersion:   *input.ExpectedDraftVersion,
			Selections:             input.Selections,
		},
	)
	if err != nil {
		s.miniAppOrdersError(ctx, w, r, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) miniAppOrdersQuote(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid orders event")
		return
	}
	var input miniAppOrdersQuoteInput
	if !decodeMiniAppOrders(w, r, &input) {
		return
	}
	if !validMiniAppOrdersQuote(input) {
		miniAppHTTPError(w, http.StatusBadRequest, "explicit versions and selections required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.OwnOrdersQuote(ctx, miniAppOrdersScope(attempt), db.OrdersQuoteArguments{
		EventID:              event,
		ExpectedEventVersion: *input.ExpectedEventVersion,
		ExpectedVersion:      *input.ExpectedCatalogVersion,
		Selections:           input.Selections,
	})
	if err != nil {
		s.miniAppOrdersError(ctx, w, r, err)
		return
	}
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *Server) miniAppOrdersError(ctx context.Context, w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		if _, sessionErr := s.currentMiniApp(ctx, r); sessionErr != nil {
			miniAppError(w, sessionErr)
			return
		}
		miniAppHTTPError(w, http.StatusNotFound, "orders event, selection operation or access not found")
		return
	}
	switch {
	case errors.Is(err, db.ErrConflict),
		errors.Is(err, orders.ErrSalesClosed),
		errors.Is(err, orders.ErrQuoteUnavailable):
		miniAppHTTPError(
			w,
			http.StatusConflict,
			"selection or catalog changed, sales closed, or complete pricing unavailable; read current state",
		)
	case errors.Is(err, orders.ErrSelection), errors.Is(err, orders.ErrInvalid), errors.Is(err, orders.ErrAmount):
		miniAppHTTPError(w, http.StatusBadRequest, "invalid selection or quote exceeds supported bounds")
	default:
		miniAppError(w, err)
	}
}
