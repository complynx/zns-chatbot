package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

type archiveStoreRequest struct {
	AuthInput

	Body struct {
		ProtocolVersion int                       `json:"protocol_version" minimum:"1" maximum:"1"`
		OperationID     uuid.UUID                 `json:"operation_id"`
		RunID           string                    `json:"run_id,omitempty"`
		Arguments       db.LegacyArchiveArguments `json:"arguments"`
		IdempotencyKey  uuid.UUID                 `json:"idempotency_key"`
		Confirmation    authorityConfirmation     `json:"confirmation"`
	}
}

type archivePreparationResponse struct {
	Body struct {
		Status        string                    `json:"status"`
		OperationID   string                    `json:"operation_id"`
		Arguments     db.LegacyArchiveArguments `json:"arguments"`
		Preconditions assignmentPreconditions   `json:"preconditions"`
		Description   string                    `json:"description"`
		ExpiresAt     time.Time                 `json:"expires_at"`
	}
}

type archiveStoreResponse struct {
	Body struct {
		Status      string                   `json:"status"`
		OperationID string                   `json:"operation_id"`
		Result      *db.LegacyArchiveReceipt `json:"result,omitempty"`
		Error       *agentRegistrationError  `json:"error,omitempty"`
	}
}

func (s *Server) registerArchiveStorage(api huma.API) {
	s.internal.HandleFunc("POST /internal/admin/legacy/store", s.storeArchiveUpload)
	huma.Register(api, huma.Operation{
		OperationID:  "agent-prepare-store-archive",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/store_archive/prepare",
		MaxBodyBytes: internalBodyLimit,
	}, s.prepareArchive)
	huma.Register(api, huma.Operation{OperationID: "agent-store-archive", Method: http.MethodPost,
		Path: "/internal/agent/administration/store_archive", MaxBodyBytes: internalBodyLimit}, s.storeArchiveFile)
	huma.Register(api, huma.Operation{
		OperationID:  "agent-store-archive-status",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/store_archive/status",
		MaxBodyBytes: internalBodyLimit,
	}, s.archiveStorageStatus)
}

func (s *Server) archiveAdminScope(ctx context.Context, auth AuthInput) (db.ProfileScope, error) {
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

func (s *Server) prepareArchive(
	ctx context.Context,
	in *agentReadRequest[db.LegacyArchiveArguments],
) (*archivePreparationResponse, error) {
	scope, err := s.archiveAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	if in.Body.Arguments.SourceFileID != uuid.Nil {
		if _, err = s.ownedFile(ctx, in.AuthInput, in.Body.Arguments.SourceFileID); err != nil {
			return nil, err
		}
	}
	prepared, err := s.store.PrepareLegacyArchive(ctx, scope, op, in.Body.Arguments)
	if err != nil {
		return nil, legacyArchiveError(err)
	}
	out := &archivePreparationResponse{}
	out.Body.Status, out.Body.OperationID, out.Body.Arguments = agentPrepared, in.Body.OperationID, prepared.Arguments
	out.Body.Preconditions.ProposalID = prepared.ID
	out.Body.ExpiresAt = prepared.ExpiresAt
	out.Body.Description = "Store the exact legacy archive SHA-256 " +
		prepared.Arguments.SHA256 +
		"; no history, users or payments will be applied."
	return out, nil
}

func (s *Server) storeArchiveFile(ctx context.Context, in *archiveStoreRequest) (*archiveStoreResponse, error) {
	scope, err := s.archiveAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	b := in.Body
	if b.OperationID == uuid.Nil || b.IdempotencyKey != b.OperationID || b.Arguments.SourceFileID == uuid.Nil ||
		b.Confirmation.Preconditions.ProposalID == uuid.Nil {
		return nil, huma.Error400BadRequest("matching operation, idempotency key, proposal and source file required")
	}
	if err = s.beginArchiveValidation(); err != nil {
		return nil, err
	}
	defer s.endArchiveValidation()
	// Metadata is checked before opening the transaction. Telegram IO is lazy so
	// receipt replay still works when Telegram no longer serves the bytes.
	record, err := s.ownedFile(ctx, in.AuthInput, b.Arguments.SourceFileID)
	if err != nil {
		return archiveStoreResult(b.OperationID.String(), db.LegacyArchiveReceipt{}, err)
	}
	open := func(readCtx context.Context) (io.ReadCloser, error) {
		raw, readErr := s.readFile(readCtx, record)
		if readErr != nil {
			return nil, readErr
		}
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	receipt, err := s.store.StoreLegacyArchive(ctx, scope, db.LegacyArchiveRequest{
		OperationID: b.OperationID,
		ProposalID:  b.Confirmation.Preconditions.ProposalID,
		Arguments:   b.Arguments,
	}, open, s.archiveIdentityCheck(in.Authorization))
	return archiveStoreResult(b.OperationID.String(), receipt, err)
}

func (s *Server) archiveIdentityCheck(auth string) func(context.Context) error {
	return func(ctx context.Context) error {
		_, err := s.authorizeIdentity(ctx, auth, true)
		return err
	}
}

func archiveStoreResult(operation string, receipt db.LegacyArchiveReceipt, err error) (*archiveStoreResponse, error) {
	out := &archiveStoreResponse{}
	out.Body.OperationID = operation
	if err != nil {
		mapped := legacyArchiveError(err)
		status, ok := errors.AsType[huma.StatusError](mapped)
		if !ok || status.GetStatus() >= http.StatusInternalServerError {
			return nil, mapped
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "archive_rejected",
			Message:        "archive, proposal, account, source or storage limit rejected",
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.OperationID, out.Body.Result = agentSucceeded, operation, &receipt
	return out, nil
}

func (s *Server) archiveStorageStatus(
	ctx context.Context,
	in *agentRegistrationStatusRequest,
) (*archiveStoreResponse, error) {
	scope, err := s.archiveAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil {
		return nil, huma.Error400BadRequest("nonzero operation UUID required")
	}
	receipt, err := s.store.LegacyArchiveOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &archiveStoreResponse{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return archiveStoreResult(in.Body.OperationID, receipt, err)
}

func legacyArchiveError(err error) error {
	if _, ok := errors.AsType[huma.StatusError](err); ok {
		return err
	}
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		return huma.Error413RequestEntityTooLarge("archive exceeds transport limit")
	}
	switch {
	case errors.Is(err, db.ErrInvalidLegacyArchive), errors.Is(err, legacyimport.ErrInvalidBundle):
		return huma.Error400BadRequest("invalid archive or storage request")
	case errors.Is(err, db.ErrConflict), errors.Is(err, db.ErrLegacyArchiveQuota):
		return huma.Error409Conflict("archive proposal, operation, bot or storage limit conflict")
	case errors.Is(err, pgx.ErrNoRows):
		return huma.Error404NotFound("archive proposal, source, account or operation unavailable")
	default:
		return huma.Error503ServiceUnavailable("archive storage unavailable; reconcile the same operation ID")
	}
}
