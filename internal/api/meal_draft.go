package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

const mealDraftPath = "/internal/agent/meals/"

type mealDraftArguments struct {
	EventID                uuid.UUID          `json:"event_id"`
	ExpectedEventVersion   int64              `json:"expected_event_version"   minimum:"1"`
	ExpectedCatalogVersion int64              `json:"expected_catalog_version" minimum:"1"`
	ExpectedDraftVersion   int64              `json:"expected_draft_version"   minimum:"0"`
	Choices                []mealDayArguments `json:"choices"                              maxItems:"14"`
}

// Optional meal fields preserve incomplete drafts and dinner-only services.
type mealDayArguments struct {
	DayID     string              `json:"day_id"`
	Lunch     *orders.LunchChoice `json:"lunch,omitempty"`
	DinnerIDs []string            `json:"dinner_ids,omitempty"`
}

func (s *Server) registerMealDraft(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-meal-draft",
			Method:       http.MethodPost,
			Path:         mealDraftPath + "draft",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentMealDraft,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-save-meal-draft",
			Method:       http.MethodPost,
			Path:         mealDraftPath + "save_draft",
			MaxBodyBytes: mealBodyLimit,
		},
		s.agentSaveMealDraft,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-meal-draft-status",
			Method:       http.MethodPost,
			Path:         mealDraftPath + "save_draft/status",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentMealDraftOperation,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-meal-draft-operation",
			Method:       http.MethodPost,
			Path:         mealDraftPath + "draft_operation",
			MaxBodyBytes: internalBodyLimit,
		},
		s.agentMealDraftOperation,
	)
}

func (s *Server) agentMealDraft(
	ctx context.Context,
	in *agentReadRequest[agentRegistrationArguments],
) (*agentReadResponse[db.MealDraft], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMealDraft(ctx, scope, in.Body.Arguments.EventID)
	if err != nil {
		return nil, mealError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) agentSaveMealDraft(
	ctx context.Context,
	in *profileWriteRequest[mealDraftArguments],
) (*paymentWriteResponse[db.MealDraft], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("operation ID must be a nonzero UUID and match idempotency key")
	}
	a := in.Body.Arguments
	var choices []orders.MealDayChoice
	if a.Choices != nil {
		choices = make([]orders.MealDayChoice, len(a.Choices))
		for i, choice := range a.Choices {
			choices[i] = orders.MealDayChoice{DayID: choice.DayID, Lunch: choice.Lunch, DinnerIDs: choice.DinnerIDs}
		}
	}
	result, err := s.store.SaveOwnMealDraft(
		ctx,
		scope,
		db.MealDraftChange{
			OperationID:            op,
			EventID:                a.EventID,
			ExpectedEventVersion:   a.ExpectedEventVersion,
			ExpectedCatalogVersion: a.ExpectedCatalogVersion,
			ExpectedDraftVersion:   a.ExpectedDraftVersion,
			Choices:                choices,
		},
	)
	return mealDraftResult(in.Body.OperationID, result, err)
}

func (s *Server) agentMealDraftOperation(
	ctx context.Context,
	in *profileOperationRequest,
) (*paymentWriteResponse[db.MealDraft], error) {
	scope, op, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.store.OwnMealDraftOperationByID(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &paymentWriteResponse[db.MealDraft]{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return mealDraftResult(in.Body.OperationID, result, err)
}

func mealDraftResult(
	operation string,
	result db.MealDraft,
	err error,
) (*paymentWriteResponse[db.MealDraft], error) {
	out := &paymentWriteResponse[db.MealDraft]{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := mealError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "meal_draft_rejected",
			Message:        "meal choices, versions or current access rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}
