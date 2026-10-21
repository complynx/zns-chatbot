package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

const historyPath = "/internal/agent/history/"

type historyApplyRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                      `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     uuid.UUID                `json:"operation_id"`
		RunID           string                   `json:"run_id,omitempty"`
		Arguments       db.HistoryApplyArguments `json:"arguments"`
		IdempotencyKey  uuid.UUID                `json:"idempotency_key"`
		Confirmation    authorityConfirmation    `json:"confirmation"`
	}
}

type historyPreparationResponse struct {
	Body struct {
		Status        string                   `json:"status"`
		OperationID   string                   `json:"operation_id"`
		Arguments     db.HistoryApplyArguments `json:"arguments"`
		Preconditions assignmentPreconditions  `json:"preconditions"`
		Description   string                   `json:"description"`
		ExpiresAt     time.Time                `json:"expires_at"`
	}
}

type historyApplyResponse struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *db.HistoryApplyReceipt `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}

type historyPageArguments struct {
	After uuid.UUID `json:"after,omitempty"`
	Limit int32     `json:"limit,omitempty" minimum:"1" maximum:"20"`
}

type historyTextArguments struct {
	ID        uuid.UUID `json:"id"`
	StartRune int32     `json:"start_rune,omitempty" minimum:"0" maximum:"8388608"`
}

func (s *Server) registerHistory(api huma.API) {
	s.registerHistoryRegistration(api)
	s.registerHistoryAvatars(api)
	huma.Register(api, huma.Operation{
		OperationID:  "agent-prepare-apply-history",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/apply_history/prepare",
		MaxBodyBytes: internalBodyLimit,
	}, s.prepareHistory)
	huma.Register(api, huma.Operation{OperationID: "agent-apply-history", Method: http.MethodPost,
		Path: "/internal/agent/administration/apply_history", MaxBodyBytes: internalBodyLimit}, s.applyHistory)
	huma.Register(api, huma.Operation{OperationID: "agent-apply-history-status", Method: http.MethodPost,
		Path: "/internal/agent/administration/apply_history/status", MaxBodyBytes: internalBodyLimit}, s.historyStatus)
	huma.Register(api, huma.Operation{OperationID: "agent-history-page", Method: http.MethodPost,
		Path: historyPath + "page", MaxBodyBytes: internalBodyLimit}, s.historyPage)
	huma.Register(api, huma.Operation{OperationID: "agent-history-text", Method: http.MethodPost,
		Path: historyPath + "text", MaxBodyBytes: internalBodyLimit}, s.historyText)
}

func (s *Server) prepareHistory(
	ctx context.Context,
	in *agentReadRequest[db.HistoryApplyArguments],
) (*historyPreparationResponse, error) {
	scope, err := s.archiveAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	prepared, err := s.store.PrepareHistoryApply(ctx, scope, op, in.Body.Arguments)
	if err != nil {
		return nil, historyError(err)
	}
	out := &historyPreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID, out.Body.ExpiresAt = prepared.ID, prepared.ExpiresAt
	out.Body.Description = "Apply historical data from archive " + prepared.Arguments.ArchiveID.String() + " SHA-256 " + prepared.Arguments.SHA256 + " using " + prepared.Arguments.ProjectionVersion + ". No live accounts, passes or confirmed money are created."
	return out, nil
}

func (s *Server) applyHistory(ctx context.Context, in *historyApplyRequest) (*historyApplyResponse, error) {
	scope, err := s.archiveAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	b := in.Body
	if b.OperationID == uuid.Nil || b.IdempotencyKey != b.OperationID ||
		b.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation, idempotency key and proposal required")
	}
	if err = s.beginArchiveValidation(); err != nil {
		return nil, err
	}
	defer s.endArchiveValidation()
	receipt, err := s.store.ApplyHistory(ctx, scope, db.HistoryApplyRequest{
		OperationID: b.OperationID,
		ProposalID:  b.Confirmation.Preconditions.ProposalID,
		Arguments:   b.Arguments,
	}, s.archiveIdentityCheck(in.Authorization))
	return historyApplyResult(b.OperationID.String(), receipt, err)
}

func (s *Server) historyStatus(ctx context.Context, in *agentRegistrationStatusRequest) (*historyApplyResponse, error) {
	scope, err := s.archiveAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	receipt, err := s.store.HistoryApplyOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &historyApplyResponse{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return historyApplyResult(in.Body.OperationID, receipt, err)
}

func historyApplyResult(operation string, receipt db.HistoryApplyReceipt, err error) (*historyApplyResponse, error) {
	out := &historyApplyResponse{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := historyError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "history_rejected",
			Message:        "archive, approval, source identity, account or history quota rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &receipt
	return out, nil
}

func historyError(err error) error {
	switch {
	case errors.Is(err, db.ErrInvalidHistory):
		return huma.Error400BadRequest("invalid historical import or read request")
	case errors.Is(err, legacyimport.ErrAvatarInput):
		return huma.Error422UnprocessableEntity("historical file is not a supported Telegram image")
	case errors.Is(err, legacyimport.ErrRegistrationScanLimit):
		return huma.Error422UnprocessableEntity("historical record exceeds discovery field limit")
	case errors.Is(err, db.ErrHistoryQuota):
		return huma.Error409Conflict("historical storage quota reached")
	default:
		return legacyArchiveError(err)
	}
}

func (s *Server) historyPage(
	ctx context.Context,
	in *agentReadRequest[historyPageArguments],
) (*agentReadResponse[db.HistoryPage], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.OwnerHistory(ctx, scope, a.After, a.Limit)
	if err != nil {
		return nil, historyError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) historyText(
	ctx context.Context,
	in *agentReadRequest[historyTextArguments],
) (*agentReadResponse[db.HistoryText], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.OwnerHistoryText(ctx, scope, a.ID, a.StartRune)
	if err != nil {
		return nil, historyError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
