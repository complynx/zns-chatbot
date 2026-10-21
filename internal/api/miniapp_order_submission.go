package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

type miniAppOrderPreparationInput struct {
	OperationID            uuid.UUID `json:"operation_id"`
	ExpectedEventVersion   *int64    `json:"expected_event_version"`
	ExpectedCatalogVersion *int64    `json:"expected_catalog_version"`
	ExpectedDraftVersion   *int64    `json:"expected_draft_version"`
}

type miniAppOrderSubmitInput struct {
	miniAppOrderPreparationInput

	ProposalID uuid.UUID `json:"proposal_id"`
	Confirmed  bool      `json:"confirmed"`
}

type miniAppOrderCancelInput struct {
	OperationID     uuid.UUID `json:"operation_id"`
	OrderID         uuid.UUID `json:"order_id"`
	ExpectedVersion *int64    `json:"expected_version"`
}

func (s *Server) registerMiniAppOrderSubmission() {
	s.registerMiniAppOrderReviews()
	s.registerMiniAppOrderEvidence()
	writes := miniAppOrderWrites{Server: s, prepare: s.store.PrepareMiniAppOrderSubmission,
		submit: s.store.SubmitMiniAppOrder, cancel: s.store.CancelMiniAppOrder, failure: s.miniAppOrdersError}
	writes.register("/app/orders/{event}/")
	s.public.HandleFunc("GET /app/orders/{event}/submitted", s.miniAppSubmittedOrders)
	s.public.HandleFunc("GET /app/orders/{event}/submitted/{order}", s.miniAppSubmittedOrder)
	s.public.HandleFunc("GET /app/orders/{event}/operation-receipts/{kind}/{operation}", s.miniAppOrderReceipt)
}

func miniAppOrderEvent(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	event, err := uuid.Parse(r.PathValue("event"))
	if err != nil || event == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid orders event")
		return uuid.Nil, false
	}
	return event, true
}

func (input miniAppOrderPreparationInput) arguments(event uuid.UUID) (db.OrderSubmission, bool) {
	if input.OperationID == uuid.Nil || input.ExpectedEventVersion == nil || input.ExpectedCatalogVersion == nil ||
		input.ExpectedDraftVersion == nil ||
		*input.ExpectedEventVersion < 1 ||
		*input.ExpectedCatalogVersion < 1 ||
		*input.ExpectedDraftVersion < 1 {
		return db.OrderSubmission{}, false
	}
	return db.OrderSubmission{
		EventID:                event,
		ExpectedEventVersion:   *input.ExpectedEventVersion,
		ExpectedCatalogVersion: *input.ExpectedCatalogVersion,
		ExpectedDraftVersion:   *input.ExpectedDraftVersion,
	}, true
}

func (s *Server) miniAppOrderSession(ctx context.Context, r *http.Request) (db.MiniAppOrderSession, error) {
	if _, err := s.authenticatedMiniApp(ctx, r); err != nil {
		return db.MiniAppOrderSession{}, err
	}
	bearer, err := miniAppBearer(r)
	if err != nil {
		return db.MiniAppOrderSession{}, err
	}
	return db.MiniAppOrderSession{Bearer: bearer, Generation: s.miniAppKey.Generation()}, nil
}

func (s *Server) miniAppOrderResult(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	result any,
	err error,
) {
	if err != nil {
		s.miniAppOrdersError(ctx, w, r, err)
		return
	}
	// A successful write may precede logout or a lost reply. Recheck delivery
	// access here; recovery must use the original key after a fresh sign-in.
	if _, err = s.currentMiniApp(ctx, r); err != nil {
		miniAppError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(result)
}

func (s *miniAppOrderWrites) miniAppPrepareOrder(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	var input miniAppOrderPreparationInput
	if !decodeMiniAppOrders(w, r, &input) {
		return
	}
	args, ok := input.arguments(event)
	if !ok {
		miniAppHTTPError(
			w,
			http.StatusBadRequest,
			"explicit operation and current event/catalog/draft versions required",
		)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.prepare(ctx, s.eventScope(), session, input.OperationID, args)
	s.miniAppOrderResult(ctx, w, r, result, err)
}

func (s *miniAppOrderWrites) miniAppSubmitOrder(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	var input miniAppOrderSubmitInput
	if !decodeMiniAppOrders(w, r, &input) {
		return
	}
	args, ok := input.arguments(event)
	if !ok || input.ProposalID == uuid.Nil || !input.Confirmed {
		miniAppHTTPError(w, http.StatusBadRequest, "explicit operation, versions, proposal and confirmation required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.submit(ctx, s.eventScope(), session, input.OperationID, args, input.ProposalID)
	s.miniAppOrderResult(ctx, w, r, result, err)
}

func (s *miniAppOrderWrites) miniAppCancelOrder(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	var input miniAppOrderCancelInput
	if !decodeMiniAppOrders(w, r, &input) {
		return
	}
	if input.OperationID == uuid.Nil || input.OrderID == uuid.Nil || input.ExpectedVersion == nil ||
		*input.ExpectedVersion < 1 {
		miniAppHTTPError(w, http.StatusBadRequest, "explicit operation, order and current revision required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.cancel(
		ctx,
		s.eventScope(),
		session,
		input.OperationID,
		db.OrderCancellation{EventID: event, OrderID: input.OrderID, ExpectedVersion: *input.ExpectedVersion},
	)
	s.miniAppOrderResult(ctx, w, r, result, err)
}
