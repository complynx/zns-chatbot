package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/loom"
)

type recoveryReconciler interface {
	ReconcileRecovery(context.Context, db.CloseTaskRequest) (db.TaskResolution, error)
}

type recoveryWithdrawer interface {
	AbandonRecovery(context.Context, db.CloseTaskRequest) (db.TaskResolution, error)
}

func (s *Server) reconcileRecovery(ctx context.Context, in *closeTaskRequest) (*closeTaskResponse, error) {
	return s.resolveRecoveryOperation(ctx, in, false)
}

func (s *Server) abandonRecovery(ctx context.Context, in *closeTaskRequest) (*closeTaskResponse, error) {
	return s.resolveRecoveryOperation(ctx, in, true)
}

func (s *Server) recoveryResolver(withdraw bool) func(context.Context, db.CloseTaskRequest) (db.TaskResolution, error) {
	if withdraw {
		if service, ok := s.tasks.(recoveryWithdrawer); ok {
			return service.AbandonRecovery
		}
	} else if service, ok := s.tasks.(recoveryReconciler); ok {
		return service.ReconcileRecovery
	}
	return nil
}

func (s *Server) resolveRecoveryOperation(ctx context.Context, in *closeTaskRequest,
	withdraw bool) (*closeTaskResponse, error) {
	p, err := s.authorize(ctx, in.Authorization, true)
	if err != nil {
		return nil, err
	}
	if !in.Body.Confirm || strings.TrimSpace(in.Body.Reason) == "" {
		return nil, huma.Error400BadRequest("explicit recovery operation confirmation and reason required")
	}
	resolve := s.recoveryResolver(withdraw)
	if resolve == nil {
		return nil, huma.Error503ServiceUnavailable("recovery operation unavailable")
	}
	result, err := resolve(ctx, db.CloseTaskRequest{
		TaskID: in.ID, BotID: s.options.BotID, Organization: s.options.OrganizationID,
		OperationID: in.Body.OperationID, Actor: p.Subject, Reason: in.Body.Reason,
		ExpectedVersion: in.Body.ExpectedVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("task not found")
	}
	if errors.Is(err, db.ErrConflict) || errors.Is(err, db.ErrLeaseLost) {
		return nil, huma.Error409Conflict("task changed, is leased, or cannot perform this recovery operation")
	}
	if errors.Is(err, identity.ErrDenied) || errors.Is(err, identity.ErrConflict) {
		return nil, huma.Error403Forbidden("customer access must be restored before retrying the retained request")
	}
	if err != nil {
		if upstream, known := errors.AsType[*loom.Error](err); known && upstream.Outcome == loom.Rejected &&
			upstream.Code != "" && upstream.StatusCode >= 400 && upstream.StatusCode < 500 {
			if upstream.StatusCode == http.StatusUnauthorized || upstream.StatusCode == http.StatusNotFound {
				return nil, huma.Error409Conflict("Loom recovery access unavailable: " + upstream.Code)
			}
			return nil, huma.NewError(upstream.StatusCode, "Loom rejected recovery reconciliation: "+upstream.Code)
		}
		return nil, huma.Error503ServiceUnavailable("recovery receipt not confirmed; retry the same operation " +
			"after restoring identity or Loom access")
	}
	return &closeTaskResponse{Body: result}, nil
}
