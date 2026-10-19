package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const ordersSelectionPath = "/internal/agent/orders/"

type ordersSelectionArguments struct {
	EventID                uuid.UUID          `json:"event_id"`
	ExpectedEventVersion   int64              `json:"expected_event_version"   minimum:"1"`
	ExpectedCatalogVersion int64              `json:"expected_catalog_version" minimum:"1"`
	ExpectedDraftVersion   int64              `json:"expected_draft_version"   minimum:"0"`
	Selections             []orders.Selection `json:"selections"                           maxItems:"100"`
}

func (s *Server) registerOrdersSelection(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-orders-selection",
			Method:       http.MethodPost,
			Path:         ordersSelectionPath + "selection",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentOrdersSelection,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-save-orders-selection",
			Method:       http.MethodPost,
			Path:         ordersSelectionPath + "save_selection",
			MaxBodyBytes: ordersCatalogBodyLimit,
		},
		s.agentSaveOrdersSelection,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-orders-selection-status",
			Method:       http.MethodPost,
			Path:         ordersSelectionPath + "save_selection/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentOrdersSelectionOperation,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-orders-selection-operation",
			Method:       http.MethodPost,
			Path:         ordersSelectionPath + "selection_operation",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentOrdersSelectionOperation,
	)
}

func (s *Server) agentOrdersSelection(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.OrdersSelection], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnOrdersSelection(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, ordersCatalogError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentSaveOrdersSelection(
	ctx context.Context,
	in *profileWriteRequest[ordersSelectionArguments],
) (*paymentWriteResponse[db.OrdersSelection], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("operation ID must be a nonzero UUID and match idempotency key")
	}
	a := in.Body.Arguments
	result, err := s.store.SaveOwnOrdersSelection(
		ctx,
		scope,
		db.OrdersSelectionChange{
			OperationID:            op,
			EventID:                a.EventID,
			ExpectedEventVersion:   a.ExpectedEventVersion,
			ExpectedCatalogVersion: a.ExpectedCatalogVersion,
			ExpectedDraftVersion:   a.ExpectedDraftVersion,
			Selections:             a.Selections,
		},
	)
	return ordersSelectionResult(in.Body.OperationID, result, err)
}

func (s *Server) agentOrdersSelectionOperation(
	ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[db.OrdersSelection], error) {
	scope, op, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnOrdersSelectionOperationByID(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.OrdersSelection]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return ordersSelectionResult(in.Body.OperationID, result, err)
}

func ordersSelectionResult(
	operation string,
	result db.OrdersSelection,
	err error,
) (*paymentWriteResponse[db.OrdersSelection], error) {
	out := &paymentWriteResponse[db.OrdersSelection]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := ordersCatalogError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "orders_selection_rejected",
			Message:        "selection, versions or current access rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

type ordersEventsArguments struct {
	After uuid.UUID `json:"after,omitempty"`
}

func (s *Server) agentOrdersEvents(
	ctx context.Context,
	in *agentReadRequest[ordersEventsArguments],
) (*agentReadResponse[events.ContextList], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.ListOwnOrdersSelectionEvents(ctx, scope, in.Body.Arguments.After)
	if err != nil {
		return nil, ordersCatalogError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
