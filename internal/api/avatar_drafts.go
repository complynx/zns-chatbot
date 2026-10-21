package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
)

type avatarDraftCreateArguments struct {
	SourceFileID  uuid.UUID               `json:"source_file_id,omitempty"`
	HistorySource *db.AvatarHistorySource `json:"history_source,omitempty"`
	Transform     *avatar.Transform       `json:"transform,omitempty"`
}

type avatarDraftIDArguments struct {
	DraftID uuid.UUID `json:"draft_id"`
}
type avatarDraftSaveArguments struct {
	DraftID         uuid.UUID        `json:"draft_id"`
	ExpectedVersion int64            `json:"expected_version" minimum:"1"`
	Transform       avatar.Transform `json:"transform"`
}
type avatarDraftResponse struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *db.AvatarDraft         `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}
type avatarDraftList struct {
	Drafts []db.AvatarDraft `json:"drafts"`
}

func (s *Server) registerAvatarDrafts(api huma.API) {
	s.registerAvatarPreview(api)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-avatar-create-draft",
			Method:       http.MethodPost,
			Path:         avatarPath + "create_draft",
			MaxBodyBytes: internalBodyLimit,
		},
		s.createAvatarDraft,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-avatar-save-draft",
			Method:       http.MethodPost,
			Path:         avatarPath + "save_draft",
			MaxBodyBytes: internalBodyLimit,
		},
		s.saveAvatarDraft,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-avatar-draft",
			Method:       http.MethodPost,
			Path:         avatarPath + "draft",
			MaxBodyBytes: internalBodyLimit,
		},
		s.avatarDraft,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-avatar-drafts",
			Method:       http.MethodPost,
			Path:         avatarPath + "drafts",
			MaxBodyBytes: internalBodyLimit,
		},
		s.avatarDrafts,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-avatar-draft-operation",
			Method:       http.MethodPost,
			Path:         avatarPath + "draft_operation",
			MaxBodyBytes: internalBodyLimit,
		},
		s.avatarDraftOperation,
	)
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-avatar-fetch-original",
			Method:       http.MethodPost,
			Path:         avatarPath + "fetch_original",
			MaxBodyBytes: internalBodyLimit,
		},
		s.avatarDraftFetch,
	)
	s.internal.HandleFunc("GET /internal/avatar-drafts/{id}/original", s.avatarDraftContent)
}
func avatarDraftOperationID(operation, key string) (uuid.UUID, error) {
	id, err := uuid.Parse(operation)
	if err != nil || id == uuid.Nil || operation != key {
		return uuid.Nil, huma.Error400BadRequest("nonzero operation ID must match idempotency key")
	}
	return id, nil
}

func (s *Server) createAvatarDraft(
	ctx context.Context,
	in *profileWriteRequest[avatarDraftCreateArguments],
) (*avatarDraftResponse, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := avatarDraftOperationID(in.Body.OperationID, in.Body.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if in.Body.Arguments.Transform != nil {
		return nil, huma.Error400BadRequest("create a draft with identity, then save its transform")
	}
	a := db.AvatarDraftCreate{
		OperationID:   op,
		SourceFileID:  in.Body.Arguments.SourceFileID,
		HistorySource: in.Body.Arguments.HistorySource,
	}
	prior, err := s.store.AvatarDraftPreflight(ctx, scope, a)
	if err != nil {
		return avatarDraftWriteResult(in.Body.OperationID, db.AvatarDraft{}, err)
	}
	if prior != nil {
		return avatarDraftWriteResult(in.Body.OperationID, *prior, nil)
	}
	raw, err := s.readAvatarCreationSource(ctx, scope, a)
	if err != nil {
		if _, ok := errors.AsType[huma.StatusError](err); ok {
			return nil, err
		}
		return avatarDraftWriteResult(in.Body.OperationID, db.AvatarDraft{}, err)
	}
	proof, err := s.validateAvatarOriginal(ctx, raw)
	if err != nil {
		return avatarDraftWriteResult(in.Body.OperationID, db.AvatarDraft{}, err)
	}
	current, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if current != scope {
		return nil, huma.Error404NotFound("avatar owner unavailable")
	}
	commit, release, err := s.avatarCreationCommitContext(ctx, scope, a)
	if err != nil {
		return avatarDraftWriteResult(in.Body.OperationID, db.AvatarDraft{}, err)
	}
	result, err := s.store.CreateAvatarDraft(commit, scope, a, raw, proof)
	release()
	return avatarDraftWriteResult(in.Body.OperationID, result, err)
}
func (s *Server) validateAvatarOriginal(ctx context.Context, raw []byte) (avatar.Rendered, error) {
	if len(raw) == 0 {
		return avatar.Rendered{}, avatar.ErrInvalidImage
	}
	if len(raw) > avatar.MaxInputBytes {
		return avatar.Rendered{}, avatar.ErrImageLimit
	}
	if s.options.AvatarRenderer == nil {
		return avatar.Rendered{}, avatar.ErrProcessorUnavailable
	}
	child, cancel := context.WithTimeout(ctx, avatarRenderTimeout)
	defer cancel()
	proof, err := s.options.AvatarRenderer.Render(child, raw, avatar.IdentityTransform())
	if err == nil {
		err = child.Err()
	}
	return proof, err
}

func (s *Server) saveAvatarDraft(
	ctx context.Context,
	in *profileWriteRequest[avatarDraftSaveArguments],
) (*avatarDraftResponse, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := avatarDraftOperationID(in.Body.OperationID, in.Body.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.SaveAvatarDraft(
		ctx,
		scope,
		db.AvatarDraftSave{
			OperationID:     op,
			DraftID:         a.DraftID,
			ExpectedVersion: a.ExpectedVersion,
			Transform:       a.Transform,
		},
	)
	return avatarDraftWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) avatarDraft(
	ctx context.Context,
	in *agentReadRequest[avatarDraftIDArguments],
) (*agentReadResponse[db.AvatarDraft], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	d, err := s.store.GetAvatarDraft(ctx, scope, in.Body.Arguments.DraftID)
	if err != nil {
		return nil, avatarReadError(err)
	}
	return agentReadResult(in.Body.OperationID, d), nil
}

func (s *Server) avatarDrafts(
	ctx context.Context,
	in *agentReadRequest[struct{}],
) (*agentReadResponse[avatarDraftList], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	drafts, err := s.store.ListAvatarDrafts(ctx, scope)
	if err != nil {
		return nil, avatarReadError(err)
	}
	return agentReadResult(in.Body.OperationID, avatarDraftList{drafts}), nil
}
func (s *Server) avatarDraftOperation(ctx context.Context, in *profileOperationRequest) (*avatarDraftResponse, error) {
	scope, op, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	d, err := s.store.AvatarDraftOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &avatarDraftResponse{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return avatarDraftWriteResult(in.Body.OperationID, d, err)
}
func avatarDraftWriteResult(operation string, d db.AvatarDraft, err error) (*avatarDraftResponse, error) {
	out := &avatarDraftResponse{}
	out.Body.OperationID = operation
	if err != nil {
		message := avatarRejection(err)
		if errors.Is(err, db.ErrAvatarDraftQuota) {
			message = "avatar draft quota reached: five active drafts, 50 MiB per owner or the configured global browser storage limit"
		}
		if message == "" {
			return nil, huma.Error503ServiceUnavailable("avatar draft unavailable")
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "avatar_draft_rejected",
			Message:        message,
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &d
	return out, nil
}

// Keep upload/read endpoints bounded independently of the default JSON guard.
const avatarUploadTimeout = 2 * time.Minute
