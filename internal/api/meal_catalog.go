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

const mealCatalogWritePath = "/internal/agent/administration/set_meal_catalog"
const mealBodyLimit = 96 << 10

// A distinct transport type avoids the domain and stored catalog sharing a
// Huma schema name; the wire fields remain the stored catalog's fields.
type mealCatalogRecord db.MealCatalog

type mealCatalogRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                   `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     uuid.UUID             `json:"operation_id"`
		RunID           string                `json:"run_id,omitempty"`
		Arguments       db.MealCatalogChange  `json:"arguments"`
		IdempotencyKey  uuid.UUID             `json:"idempotency_key"`
		Confirmation    authorityConfirmation `json:"confirmation"`
	}
}

type mealCatalogPreparationResponse struct {
	Body struct {
		Status        string                  `json:"status"`
		OperationID   string                  `json:"operation_id"`
		Arguments     db.MealCatalogChange    `json:"arguments"`
		Preconditions assignmentPreconditions `json:"preconditions"`
		Description   string                  `json:"description"`
		ExpiresAt     time.Time               `json:"expires_at"`
	}
}

func (s *Server) registerMealCatalog(api huma.API) {
	s.registerMealDraft(api)
	s.registerMealSubmission(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-own-meal-catalog",
			Method:       http.MethodPost,
			Path:         "/internal/agent/meals/catalog",
			MaxBodyBytes: internalBodyLimit,
		},
		s.ownMealCatalog,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-admin-meal-catalog",
			Method:       http.MethodPost,
			Path:         "/internal/agent/administration/meal_catalog",
			MaxBodyBytes: internalBodyLimit,
		},
		s.adminMealCatalog,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-prepare-meal-catalog",
			Method:       http.MethodPost,
			Path:         mealCatalogWritePath + "/prepare",
			MaxBodyBytes: mealBodyLimit,
		},
		s.prepareMealCatalog,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-set-meal-catalog",
			Method:       http.MethodPost,
			Path:         mealCatalogWritePath,
			MaxBodyBytes: mealBodyLimit,
		},
		s.setMealCatalog,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-meal-catalog-status",
			Method:       http.MethodPost,
			Path:         mealCatalogWritePath + "/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.mealCatalogStatus,
	)
}

type mealCatalogArguments struct {
	EventID uuid.UUID `json:"event_id"`
	Version int64     `json:"version,omitempty" minimum:"0"`
}

func (s *Server) ownMealCatalog(
	ctx context.Context,
	in *agentReadRequest[mealCatalogArguments],
) (*agentReadResponse[db.MealCatalogView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMealCatalog(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.Version)
	if err != nil {
		return nil, mealError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) adminMealCatalog(
	ctx context.Context,
	in *agentReadRequest[mealCatalogArguments],
) (*agentReadResponse[mealCatalogRecord], error) {
	scope, err := s.ordersAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.MealCatalogConfiguration(ctx, scope, in.Body.Arguments.EventID, in.Body.Arguments.Version)
	if err != nil {
		return nil, mealError(err)
	}
	return agentReadResult(in.Body.OperationID, mealCatalogRecord(result)), nil
}

func (s *Server) prepareMealCatalog(
	ctx context.Context,
	in *agentReadRequest[db.MealCatalogChange],
) (*mealCatalogPreparationResponse, error) {
	scope, err := s.ordersAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	prepared, err := s.store.PrepareMealCatalog(ctx, scope, op, in.Body.Arguments)
	if err != nil {
		return nil, mealError(err)
	}
	out := &mealCatalogPreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = fmt.Sprintf(
		"Replace meal catalog for event %s at event version %d, catalog version %d. Currency %s, %d days. All prices include mandatory charges. Existing saved receipts remain historical. No order, reservation or payment changes.",
		prepared.Arguments.EventID,
		prepared.Arguments.ExpectedEventVersion,
		prepared.Arguments.ExpectedVersion,
		prepared.Arguments.Document.Currency,
		len(prepared.Arguments.Document.Days),
	)
	return out, nil
}

func (s *Server) setMealCatalog(
	ctx context.Context,
	in *mealCatalogRequest,
) (*paymentWriteResponse[mealCatalogRecord], error) {
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
	result, err := s.store.ChangeMealCatalog(
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
	return mealCatalogResult(b.OperationID.String(), result, err)
}

func (s *Server) mealCatalogStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*paymentWriteResponse[mealCatalogRecord], error) {
	scope, err := s.ordersAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	result, err := s.store.MealCatalogOperationByID(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[mealCatalogRecord]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return mealCatalogResult(in.Body.OperationID, result, err)
}

func mealError(err error) error {
	switch {
	case errors.Is(err, db.ErrMealDeadlineMissing):
		return huma.Error409Conflict("meal payment deadline is not configured")
	case errors.Is(err, db.ErrEmptyMealOrder):
		return huma.Error400BadRequest("meal order has no selections")
	case errors.Is(err, orders.ErrSalesClosed):
		return huma.Error409Conflict("changed meal service sales are closed")
	case errors.Is(err, orders.ErrMealCatalog),
		errors.Is(err, orders.ErrMealChoices),
		errors.Is(err, orders.ErrAmount),
		errors.Is(err, orders.ErrSelection):
		return huma.Error400BadRequest("invalid meal catalog or choices")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("meal catalog, draft or entitlement not found")
	case errors.Is(err, db.ErrConflict):
		return huma.Error409Conflict("meal, event version or approval changed")
	default:
		if _, ok := errors.AsType[huma.StatusError](err); ok {
			return err
		}
		return huma.Error503ServiceUnavailable("meal storage unavailable")
	}
}

func mealCatalogResult(
	operation string,
	result db.MealCatalog,
	err error,
) (*paymentWriteResponse[mealCatalogRecord], error) {
	out := &paymentWriteResponse[mealCatalogRecord]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := mealError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "meal_catalog_rejected",
			Message:        "catalog, versions, approval or account rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	record := mealCatalogRecord(result)
	out.Body.Status, out.Body.Result = agentSucceeded, &record
	return out, nil
}
