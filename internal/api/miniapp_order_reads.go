package api

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

func miniAppOrderPage(r *http.Request) (uuid.UUID, int32, bool) {
	after := uuid.Nil
	if raw := r.URL.Query().Get("after"); raw != "" {
		var err error
		after, err = uuid.Parse(raw)
		if err != nil || after == uuid.Nil {
			return uuid.Nil, 0, false
		}
	}
	limit := int64(agentTaskPageLimit)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var err error
		limit, err = strconv.ParseInt(raw, 10, 32)
		if err != nil || limit < 1 || limit > agentTaskPageLimit {
			return uuid.Nil, 0, false
		}
	}
	return after, int32(limit), true
}

func (s *Server) miniAppSubmittedOrders(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	after, limit, ok := miniAppOrderPage(r)
	if !ok {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid orders page cursor or limit")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.ListOwnOrders(ctx, miniAppOrdersScope(attempt), event, after, limit)
	if err == nil {
		result, err = boundedOrdersPage(result)
	}
	s.miniAppOrderResult(ctx, w, r, result, err)
}

func (s *Server) miniAppSubmittedOrder(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	order, err := uuid.Parse(r.PathValue(schemaOrder))
	if err != nil || order == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid submitted order")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.OwnOrder(ctx, miniAppOrdersScope(attempt), event, order)
	s.miniAppOrderResult(ctx, w, r, result, err)
}

func (s *Server) miniAppOrderReceipt(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	kind := db.OrderOperationKind(r.PathValue("kind"))
	if kind != db.OrderPrepare && kind != db.OrderSubmit && kind != db.OrderCancel {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid order operation phase")
		return
	}
	operation, err := uuid.Parse(r.PathValue("operation"))
	if err != nil || operation == uuid.Nil {
		miniAppHTTPError(w, http.StatusBadRequest, "invalid order operation")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	attempt, err := s.authenticatedMiniApp(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	result, err := s.store.OwnOrderOperation(ctx, miniAppOrdersScope(attempt), event, operation, kind)
	s.miniAppOrderResult(ctx, w, r, result, err)
}
