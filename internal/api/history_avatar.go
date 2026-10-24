package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

const historyAvatarFetchMethod = "avatar_fetch"

const historyAvatarReadTimeout = 2*archiveValidationTimeout + 20*time.Second

type historyAvatarArguments struct {
	RecordID  uuid.UUID `json:"record_id"`
	ArchiveID uuid.UUID `json:"archive_id"`
}

func (s *Server) registerHistoryAvatars(api huma.API) {
	registerAgentPost(
		api,
		"agent-history-avatar-refetch",
		historyPath+historyAvatarRefetchMethod,
		internalBodyLimit,
		s.refetchHistoryAvatar,
	)
	huma.Register(api, huma.Operation{OperationID: "agent-history-avatar-candidates", Method: http.MethodPost,
		Path: historyPath + "avatar_candidates", MaxBodyBytes: internalBodyLimit}, s.historyAvatarCandidates)

	huma.Register(api, huma.Operation{OperationID: "agent-history-avatar-info", Method: http.MethodPost,
		Path: historyPath + "avatar_info", MaxBodyBytes: internalBodyLimit}, s.historyAvatarInfo)
	huma.Register(api, huma.Operation{OperationID: "agent-history-avatar-fetch", Method: http.MethodPost,
		Path: historyPath + historyAvatarFetchMethod, MaxBodyBytes: internalBodyLimit}, s.historyAvatarFetch)
	s.internal.HandleFunc("GET /internal/history-avatars/{archive}/{id}/content", s.historyAvatarContent)
}

func (s *Server) historyAvatarSource(ctx context.Context, auth AuthInput,
	a historyAvatarArguments,
) (db.HistoryAvatarSource, error) {
	ctx, cancel := context.WithTimeout(ctx, archiveValidationTimeout)
	defer cancel()
	scope, err := s.profileScope(ctx, auth)
	if err != nil {
		return db.HistoryAvatarSource{}, err
	}
	if err = s.beginArchiveValidation(); err != nil {
		return db.HistoryAvatarSource{}, err
	}
	defer s.endArchiveValidation()
	source, err := s.store.OwnerHistoryAvatarSource(ctx, scope, a.RecordID, a.ArchiveID,
		s.reauthorizeHistoryOwner(auth, scope))
	if err != nil {
		return db.HistoryAvatarSource{}, historyError(err)
	}
	return source, nil
}

func (s *Server) historyAvatarInfo(ctx context.Context, in *agentReadRequest[historyAvatarArguments],
) (*agentReadResponse[db.HistoryAvatarSource], error) {
	source, err := s.historyAvatarSource(ctx, in.AuthInput, in.Body.Arguments)
	if err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, source), nil
}

func (s *Server) historyAvatarFetch(ctx context.Context, in *agentReadRequest[historyAvatarArguments],
) (*fileEnvelope, error) {
	ctx, cancel := context.WithTimeout(ctx, historyAvatarReadTimeout)
	defer cancel()
	a := in.Body.Arguments
	source, err := s.historyAvatarSource(ctx, in.AuthInput, a)
	if err != nil {
		return nil, err
	}
	raw, err := s.readFile(ctx, db.File{TelegramID: source.FileID})
	if err != nil {
		return nil, err
	}
	current, err := s.historyAvatarSource(ctx, in.AuthInput, a)
	if err != nil {
		return nil, err
	}
	if current.FileID != source.FileID || current.UniqueID != source.UniqueID {
		return nil, huma.Error409Conflict("historical image reference changed")
	}
	digest := sha256.Sum256(raw)
	result := struct {
		Status      string                 `json:"status"`
		OperationID string                 `json:"operation_id"`
		Result      db.HistoryAvatarSource `json:"result"`
		Artifacts   []fileDescriptor       `json:"artifacts"`
	}{agentSucceeded, in.Body.OperationID, source, []fileDescriptor{{
		ExternalID: a.ArchiveID.String() + ":" + a.RecordID.String(), FetchMethod: historyAvatarRefetchMethod,
		DownloadPath: "/internal/history-avatars/" + a.ArchiveID.String() + "/" + a.RecordID.String() + "/content",
		MIMEType:     fileContentType(raw), SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(digest[:]),
	}}}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("historical image descriptor unavailable")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: body}, nil
}

func (s *Server) historyAvatarContent(w http.ResponseWriter, r *http.Request) {
	archiveID, err := uuid.Parse(r.PathValue("archive"))
	if err != nil || archiveID == uuid.Nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), historyAvatarReadTimeout)
	defer cancel()
	s.serveFileContent(w, r.WithContext(ctx), func(ctx context.Context, auth AuthInput, id uuid.UUID) (db.File, error) {
		source, readErr := s.historyAvatarSource(ctx, auth, historyAvatarArguments{RecordID: id, ArchiveID: archiveID})
		if readErr != nil {
			return db.File{}, readErr
		}
		return db.File{TelegramID: source.FileID}, nil
	})
}

func (s *Server) historyAvatarCandidates(ctx context.Context, in *agentReadRequest[historyPageArguments],
) (*agentReadResponse[db.HistoryAvatarCandidates], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	a := in.Body.Arguments
	result, err := s.store.OwnerHistoryAvatarCandidates(ctx, scope, a.After, a.Limit)
	if err != nil {
		return nil, historyError(err)
	}
	if err = s.reauthorizeHistoryOwner(in.AuthInput, scope)(ctx); err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, result), nil
}
