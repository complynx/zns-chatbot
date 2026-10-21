package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Native financial reads keep the session check inside the business transaction.
// Delivery repeats current access after that transaction, as for other Mini App reads.
func (s *Server) miniAppSubmittedMeals(w http.ResponseWriter, r *http.Request) {
	if !s.miniAppRequest(w, r) {
		return
	}
	event, ok := miniAppOrderEvent(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), miniAppTimeout)
	defer cancel()
	session, err := s.miniAppOrderSession(ctx, r)
	if err != nil {
		miniAppError(w, err)
		return
	}
	var result any
	switch {
	case r.PathValue("kind") != "":
		kind := db.OrderOperationKind(r.PathValue("kind"))
		op, parseErr := uuid.Parse(r.PathValue("operation"))
		if parseErr != nil || op == uuid.Nil ||
			(kind != db.OrderPrepare && kind != db.OrderSubmit && kind != db.OrderCancel) {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid meal order operation or phase")
			return
		}
		result, err = s.store.MiniAppOwnMealOrderOperation(ctx, s.eventScope(), session, event, op, kind)
	case r.PathValue("order") != "":
		order, parseErr := uuid.Parse(r.PathValue("order"))
		if parseErr != nil || order == uuid.Nil {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid meal order")
			return
		}
		result, err = s.store.MiniAppOwnMealOrder(ctx, s.eventScope(), session, event, order)
	default:
		after, limit, valid := miniAppOrderPage(r)
		if !valid {
			miniAppHTTPError(w, http.StatusBadRequest, "invalid meal order page cursor or limit")
			return
		}
		var page db.OrdersPage
		page, err = s.store.MiniAppOwnMealOrders(ctx, s.eventScope(), session, event, after, limit)
		if err == nil {
			page, err = boundedOrdersPage(page)
		}
		result = page
	}
	if err != nil {
		s.miniAppMealsError(ctx, w, r, err)
		return
	}
	s.miniAppOrderResult(ctx, w, r, result, nil)
}
