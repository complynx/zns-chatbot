package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

type avatarFinalizationResponse struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *db.AvatarDraftReceipt  `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}

func (s *Server) registerAvatarFinalization(api huma.API) {
	registerAgentPost(
		api,
		"agent-avatar-finalize-draft",
		avatarPath+"finalize_draft",
		internalBodyLimit,
		s.finalizeAvatarDraft,
	)
	registerAgentPost(
		api,
		"agent-avatar-finalization-operation",
		avatarPath+"finalization_operation",
		internalBodyLimit,
		s.avatarFinalizationOperation,
	)
	registerAgentPost(api, "agent-avatar-finalized", avatarPath+"finalized", internalBodyLimit, s.finalizedAvatar)
	registerAgentPost(
		api,
		"agent-avatar-fetch-finalized",
		avatarPath+"fetch_finalized",
		internalBodyLimit,
		s.fetchFinalizedAvatar,
	)
	s.internal.HandleFunc("GET /internal/finalized-avatars/{id}/content", s.finalizedAvatarContent)
	registerAgentPost(
		api,
		"agent-avatar-finalized-list",
		avatarPath+"finalized_list",
		internalBodyLimit,
		s.listFinalizedAvatars,
	)
	s.registerMiniAppAvatarFinalization()
}

// renderAvatarFinalization skips rendering committed requests. Both callers still
// pass through their commit method, which checks current authority before replay.
func (s *Server) renderAvatarFinalization(
	ctx context.Context,
	scope db.ProfileScope,
	a db.AvatarDraftFinalize,
) (db.AvatarDraftRender, error) {
	prior, err := s.store.AvatarDraftFinalizePreflight(ctx, scope, a)
	if err != nil || prior != nil {
		return db.AvatarDraftRender{}, err
	}
	_, rendered, err := s.renderAvatarDraft(ctx, scope, a.DraftID, a.ExpectedVersion)
	if err != nil {
		// Another exact request may have committed while this render was running.
		// Reconcile it before declaring a stale draft or processing error terminal.
		committed, checkErr := s.store.AvatarDraftFinalizePreflight(ctx, scope, a)
		if checkErr != nil {
			return db.AvatarDraftRender{}, checkErr
		}
		if committed != nil {
			return db.AvatarDraftRender{}, nil
		}
	}
	return rendered, err
}

func (s *Server) finalizeAvatarDraft(
	ctx context.Context,
	in *profileWriteRequest[avatarPreviewArguments],
) (*avatarFinalizationResponse, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := avatarDraftOperationID(in.Body.OperationID, in.Body.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	a := db.AvatarDraftFinalize{
		OperationID:     op,
		DraftID:         in.Body.Arguments.DraftID,
		ExpectedVersion: in.Body.Arguments.ExpectedVersion,
	}
	rendered, err := s.renderAvatarFinalization(ctx, scope, a)
	if err != nil {
		return avatarFinalizationResult(in.Body.OperationID, db.AvatarDraftReceipt{}, err)
	}
	current, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if current != scope {
		return nil, huma.Error404NotFound("avatar owner unavailable")
	}
	commit, release, err := s.avatarFinalizationCommitContext(ctx, scope, a)
	if err != nil {
		return avatarFinalizationResult(in.Body.OperationID, db.AvatarDraftReceipt{}, err)
	}
	receipt, err := s.store.CommitAvatarDraft(commit, scope, a, rendered)
	release()
	return avatarFinalizationResult(in.Body.OperationID, receipt, err)
}

func avatarFinalizationResult(
	operation string,
	result db.AvatarDraftReceipt,
	err error,
) (*avatarFinalizationResponse, error) {
	out := &avatarFinalizationResponse{}
	out.Body.OperationID = operation
	if err != nil {
		message := avatarRejection(err)
		if errors.Is(err, pgx.ErrNoRows) {
			message = "saved avatar draft or operation unavailable"
		}
		if errors.Is(err, db.ErrConflict) || errors.Is(err, errAvatarPreviewFrame) {
			message = "saved avatar draft changed or operation conflicts; read current state"
		}
		if message == "" {
			return nil, huma.Error503ServiceUnavailable("avatar finalization unavailable")
		}
		out.Body.Status = agentRejected
		out.Body.Error = &agentRegistrationError{
			Code:           "avatar_finalization_rejected",
			Message:        message,
			Classification: agentTerminalRejection,
		}
		return out, nil
	}
	out.Body.Status = agentSucceeded
	out.Body.Result = &result
	return out, nil
}

func (s *Server) avatarFinalizationOperation(
	ctx context.Context,
	in *profileOperationRequest,
) (*avatarFinalizationResponse, error) {
	scope, op, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	receipt, err := s.store.AvatarDraftFinalizationOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &avatarFinalizationResponse{}
		out.Body.Status = agentNotFound
		out.Body.OperationID = in.Body.OperationID
		return out, nil
	}
	return avatarFinalizationResult(in.Body.OperationID, receipt, err)
}

func (s *Server) finalizedAvatar(
	ctx context.Context,
	in *agentReadRequest[avatarArguments],
) (*agentReadResponse[db.AvatarFinalizationView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.FinalizedAvatarStatus(ctx, scope, in.Body.Arguments.ArtifactID)
	if err != nil {
		return nil, avatarReadError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
func finalizedAvatarPath(receipt db.AvatarDraftReceipt) string {
	return "/internal/finalized-avatars/" + receipt.ArtifactID.String() + "/content"
}

func (s *Server) fetchFinalizedAvatar(
	ctx context.Context,
	in *agentReadRequest[avatarArguments],
) (*fileEnvelope, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	view, err := s.store.FinalizedAvatarStatus(ctx, scope, in.Body.Arguments.ArtifactID)
	if err != nil {
		return nil, avatarReadError(err)
	}
	result := struct {
		Status      string                `json:"status"`
		OperationID string                `json:"operation_id"`
		Result      db.AvatarDraftReceipt `json:"result"`
		Artifacts   []fileDescriptor      `json:"artifacts"`
	}{agentSucceeded, in.Body.OperationID, view.Avatar, []fileDescriptor{{ExternalID: "avatar-finalized:" + view.Avatar.ArtifactID.String(), FetchMethod: avatarRefetchMethod, DownloadPath: finalizedAvatarPath(view.Avatar), MIMEType: mimeJPEG, SizeBytes: view.Avatar.SizeBytes, SHA256: view.Avatar.SHA256}}}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("avatar descriptor unavailable")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: raw}, nil
}
func (s *Server) finalizedAvatarContent(w http.ResponseWriter, r *http.Request) {
	id, err := avatarDraftLocator(r)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	auth := AuthInput{Authorization: r.Header.Get("Authorization")}
	scope, err := s.profileScope(r.Context(), auth)
	if err != nil {
		fileHTTPError(w, err)
		return
	}
	_, raw, err := s.store.GetFinalizedAvatar(r.Context(), scope, id)
	if err != nil {
		fileHTTPError(w, avatarReadError(err))
		return
	}
	current, err := s.profileScope(r.Context(), auth)
	if err != nil {
		fileHTTPError(w, err)
		return
	}
	if current != scope {
		http.NotFound(w, r)
		return
	}
	writeFinalizedAvatar(w, raw)
}
func writeFinalizedAvatar(w http.ResponseWriter, raw []byte) {
	w.Header().Set("Content-Type", mimeJPEG)
	w.Header().Set("Content-Disposition", `attachment; filename="zinusya-avatar.jpg"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(raw) // #nosec G705 -- owner-authorized JPEG attachment with nosniff and sandbox CSP.
}

func avatarDraftLocator(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil || id == uuid.Nil {
		return uuid.Nil, db.ErrInvalidAvatar
	}
	return id, nil
}

type avatarFinalizationList struct {
	Avatars []db.AvatarFinalizationView `json:"avatars"`
}

func (s *Server) listFinalizedAvatars(
	ctx context.Context,
	in *agentReadRequest[struct{}],
) (*agentReadResponse[avatarFinalizationList], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.ListFinalizedAvatars(ctx, scope)
	if err != nil {
		return nil, avatarReadError(err)
	}
	return agentReadResult(in.Body.OperationID, avatarFinalizationList{Avatars: rows}), nil
}
