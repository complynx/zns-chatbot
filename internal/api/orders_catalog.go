package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const ordersCatalogWritePath = "/internal/agent/administration/set_orders_catalog"
const ordersCatalogBodyLimit = 64 << 10

type ordersCatalogRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                    `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     uuid.UUID              `json:"operation_id"`
		RunID           string                 `json:"run_id,omitempty"`
		Arguments       db.OrdersCatalogChange `json:"arguments"`
		IdempotencyKey  uuid.UUID              `json:"idempotency_key"`
		Confirmation    authorityConfirmation  `json:"confirmation"`
	}
}

type ordersCatalogPreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.OrdersCatalogChange  `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

func (s *Server) registerOrdersCatalog(api huma.API) {
	s.registerOrdersSelection(api)
	s.registerOrderSubmission(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-orders-events",
			Method:       http.MethodPost,
			Path:         ordersSelectionPath + "events",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentOrdersEvents,
	)
	huma.Register(api, huma.Operation{OperationID: "agent-own-orders-quote", Method: http.MethodPost,
		Path: "/internal/agent/orders/quote", MaxBodyBytes: ordersCatalogBodyLimit}, s.ownOrdersQuote)
	huma.Register(api, huma.Operation{OperationID: "agent-own-orders-catalog", Method: http.MethodPost,
		Path: "/internal/agent/orders/catalog", MaxBodyBytes: internalBodyLimit}, s.ownOrdersCatalog)
	huma.Register(api, huma.Operation{OperationID: "agent-admin-orders-catalog", Method: http.MethodPost,
		Path: "/internal/agent/administration/orders_catalog", MaxBodyBytes: internalBodyLimit}, s.adminOrdersCatalog)
	huma.Register(api, huma.Operation{OperationID: "agent-prepare-orders-catalog", Method: http.MethodPost,
		Path: ordersCatalogWritePath + "/prepare", MaxBodyBytes: ordersCatalogBodyLimit}, s.prepareOrdersCatalog)
	huma.Register(api, huma.Operation{OperationID: "agent-set-orders-catalog", Method: http.MethodPost,
		Path: ordersCatalogWritePath, MaxBodyBytes: ordersCatalogBodyLimit}, s.setOrdersCatalog)
	huma.Register(api, huma.Operation{OperationID: "agent-orders-catalog-status", Method: http.MethodPost,
		Path: ordersCatalogWritePath + "/status", MaxBodyBytes: internalBodyLimit}, s.ordersCatalogStatus)
}

func (s *Server) ownOrdersQuote(
	ctx context.Context,
	in *agentReadRequest[db.OrdersQuoteArguments],
) (*agentReadResponse[db.OrdersQuoteView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnOrdersQuote(ctx, scope, in.Body.Arguments)
	if err != nil {
		return nil, ordersCatalogError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) ordersAdminScope(ctx context.Context, auth AuthInput) (db.ProfileScope, error) {
	p, err := s.authorize(ctx, auth.Authorization, true)
	if err != nil {
		return db.ProfileScope{}, err
	}
	u, err := s.account(ctx, p)
	if err != nil {
		return db.ProfileScope{}, err
	}
	return db.ProfileScope{
		BotID:        s.options.BotID,
		Organization: s.options.OrganizationID,
		Subject:      p.Subject,
		UserID:       u.ID,
	}, nil
}

func (s *Server) ownOrdersCatalog(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.OrdersCatalogView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnOrdersCatalog(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, ordersCatalogError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) adminOrdersCatalog(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.OrdersCatalog], error) {
	scope, err := s.ordersAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OrdersCatalogConfiguration(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, ordersCatalogError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) prepareOrdersCatalog(
	ctx context.Context,
	in *agentReadRequest[db.OrdersCatalogChange],
) (*ordersCatalogPreparationResponse, error) {
	scope, err := s.ordersAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	prepared, err := s.store.PrepareOrdersCatalog(ctx, scope, op, in.Body.Arguments)
	if err != nil {
		return nil, ordersCatalogError(err)
	}
	out := &ordersCatalogPreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = fmt.Sprintf(
		"Replace orders catalog for event %s at event version %d, catalog version %d. "+
			"Currency %s, %d items, %d offers. Prices are base prices; "+
			"declared resource ceilings are configuration only. "+
			"No order occupancy, reservation or money change.",
		prepared.Arguments.EventID,
		prepared.Arguments.ExpectedEventVersion,
		prepared.Arguments.ExpectedVersion,
		prepared.Arguments.Document.Currency,
		len(prepared.Arguments.Document.Items),
		len(prepared.Arguments.Document.Offers),
	)
	return out, nil
}

func (s *Server) setOrdersCatalog(
	ctx context.Context,
	in *ordersCatalogRequest,
) (*paymentWriteResponse[db.OrdersCatalog], error) {
	scope, err := s.ordersAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	b := in.Body
	if err = requireExactOperation(
		b.OperationID,
		b.IdempotencyKey,
		b.Confirmation.Preconditions.ProposalID,
	); err != nil {
		return nil, err
	}
	result, err := s.store.ChangeOrdersCatalog(
		ctx,
		scope,
		b.OperationID,
		b.Confirmation.Preconditions.ProposalID,
		b.Arguments,
		func(checkCtx context.Context) error {
			_, checkErr := s.authorizeIdentity(checkCtx, in.Authorization, true)
			return checkErr
		},
	)
	return ordersCatalogResult(b.OperationID.String(), result, err)
}

func (s *Server) ordersCatalogStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[db.OrdersCatalog], error) {
	scope, err := s.ordersAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	result, err := s.store.OrdersCatalogOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.OrdersCatalog]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return ordersCatalogResult(in.Body.OperationID, result, err)
}

func ordersCatalogError(err error) error {
	switch {
	case errors.Is(err, orders.ErrQuoteUnavailable):
		return huma.Error409Conflict("quote_unavailable: complete pricing is not configured")
	case errors.Is(err, orders.ErrSalesClosed):
		return huma.Error409Conflict("selected offer sales are closed")
	case errors.Is(err, orders.ErrSelection), errors.Is(err, orders.ErrAmount):
		return huma.Error400BadRequest("invalid selections or quote exceeds supported bounds")
	case errors.Is(err, orders.ErrInvalid):
		return huma.Error400BadRequest("invalid orders catalog")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("orders catalog or entitlement not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("catalog, event version or approval changed")
	default:
		if _, ok := errors.AsType[huma.StatusError](err); ok {
			return err
		}
		return huma.Error503ServiceUnavailable("orders catalog storage unavailable")
	}
}

func ordersCatalogResult(
	operation string,
	result db.OrdersCatalog,
	err error,
) (*paymentWriteResponse[db.OrdersCatalog], error) {
	out := &paymentWriteResponse[db.OrdersCatalog]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := ordersCatalogError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "orders_catalog_rejected",
			Message:        "catalog, versions, approval or account rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
