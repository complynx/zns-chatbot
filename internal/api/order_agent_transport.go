package api

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Both modules share HTTP validation and recovery, while each store binding
// retains its own entitlement checks, immutable draft and persisted business kind.
type orderAgentActions struct {
	prepare   func(context.Context, db.ProfileScope, uuid.UUID, db.OrderSubmission, db.OrderSource) (db.OrderPreparation, error)
	submit    func(context.Context, db.ProfileScope, uuid.UUID, db.OrderSubmission, uuid.UUID, db.OrderSource) (db.OrderSubmissionResult, error)
	cancel    func(context.Context, db.ProfileScope, uuid.UUID, db.OrderCancellation, db.OrderSource) (db.SubmittedOrder, error)
	get       func(context.Context, db.ProfileScope, uuid.UUID, uuid.UUID) (db.SubmittedOrder, error)
	list      func(context.Context, db.ProfileScope, uuid.UUID, uuid.UUID, int32) (db.OrdersPage, error)
	operation func(context.Context, db.ProfileScope, uuid.UUID, db.OrderOperationKind) (db.OrderOperation, error)
}

type orderAgentAPI struct {
	*Server

	actions  orderAgentActions
	mapError func(error) error
	describe func(db.OrderPreparation, string) (string, error)
}

func (s *Server) registerOrderSubmission(api huma.API) {
	h := orderAgentAPI{Server: s, mapError: ordersCatalogError, describe: orderSubmissionDescription,
		actions: orderAgentActions{
			prepare: s.store.PrepareOwnOrderSubmission, submit: s.store.SubmitOwnOrder,
			cancel: s.store.CancelOwnOrder, get: s.store.OwnOrder, list: s.store.ListOwnOrders,
			operation: s.store.OwnOrderOperationByID,
		}}
	h.register(api, ordersSelectionPath, "agent-orders-")
}

func (s *Server) registerMealSubmission(api huma.API) {
	h := orderAgentAPI{Server: s, mapError: mealError, describe: mealOrderSubmissionDescription,
		actions: orderAgentActions{
			prepare: s.store.PrepareOwnMealOrderSubmission, submit: s.store.SubmitOwnMealOrder,
			cancel: s.store.CancelOwnMealOrder, get: s.store.OwnMealOrder, list: s.store.ListOwnMealOrders,
			operation: s.store.OwnMealOrderOperationByID,
		}}
	h.register(api, mealDraftPath, "agent-meals-")
}
