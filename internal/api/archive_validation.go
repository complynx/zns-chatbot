package api

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

const (
	archiveValidationTimeout = 30 * time.Second
	schemaSHA256             = "sha256"
	schemaArchiveID          = "archive_id"
	schemaSHA256Pattern      = "^[0-9a-f]{64}$"
)

type archiveValidationArguments struct {
	SourceFileID uuid.UUID `json:"source_file_id"`
	SHA256       string    `json:"sha256"         minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$"`
}

func (s *Server) registerArchiveValidation(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:  "agent-admin-validate-stored-archive",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/validate_stored_archive",
		MaxBodyBytes: internalBodyLimit,
	}, s.validateStoredArchive)
	s.internal.HandleFunc("POST /internal/admin/legacy/validate", s.validateArchiveUpload)
	huma.Register(api, huma.Operation{
		OperationID:  "agent-admin-validate-archive",
		Method:       http.MethodPost,
		Path:         "/internal/agent/administration/validate_archive",
		MaxBodyBytes: internalBodyLimit,
	}, s.agentValidateArchive)
}

type storedArchiveArguments struct {
	ArchiveID uuid.UUID `json:"archive_id"`
}

func (s *Server) validateStoredArchive(
	ctx context.Context,
	in *agentReadRequest[storedArchiveArguments],
) (*agentReadResponse[legacyimport.BundleValidation], error) {
	scope, err := s.archiveAdminScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	if err = s.beginArchiveValidation(); err != nil {
		return nil, err
	}
	defer s.endArchiveValidation()
	result, err := s.store.ValidateStoredLegacyArchive(ctx, scope, in.Body.Arguments.ArchiveID)
	if err != nil {
		return nil, legacyArchiveError(err)
	}
	if _, err = s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func storedArchiveValidationMethod() map[string]any {
	method := administrativeRead(
		"validate_stored_archive",
		"Recheck a stored legacy archive from its exact database bytes. No histor"+
			"y is applied and no source records are exposed.",
		map[string]any{
			schemaArchiveID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
		},
		[]string{schemaArchiveID},
		[]string{"archive", "inspection"},
	)
	method[manifestTimeout] = archiveValidationTimeout.Milliseconds()
	return method
}

func (s *Server) agentValidateArchive(ctx context.Context, in *agentReadRequest[archiveValidationArguments]) (
	*agentReadResponse[legacyimport.BundleValidation], error,
) {
	ctx, cancel := context.WithTimeout(ctx, archiveValidationTimeout)
	defer cancel()
	if _, err := s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	if err := s.beginArchiveValidation(); err != nil {
		return nil, err
	}
	defer s.endArchiveValidation()
	a := in.Body.Arguments
	record, err := s.ownedFile(ctx, in.AuthInput, a.SourceFileID)
	if err != nil {
		return nil, err
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		return nil, err
	}
	result, err := s.validateArchive(ctx, bytes.NewReader(raw), a.SHA256)
	if err != nil {
		return nil, err
	}
	if _, err = s.authorize(ctx, in.Authorization, true); err != nil {
		return nil, err
	}
	if _, err = s.ownedFile(ctx, in.AuthInput, a.SourceFileID); err != nil {
		return nil, err
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) beginArchiveValidation() error {
	select {
	case s.archiveValidation <- struct{}{}:
		return nil
	default:
		return huma.Error503ServiceUnavailable("archive validator busy")
	}
}

func (s *Server) endArchiveValidation() { <-s.archiveValidation }

func archiveValidationMethod() map[string]any {
	method := administrativeRead(
		"validate_archive",
		"Verify a caller-owned legacy archive using its source_file_id and expect"+
			"ed SHA-256. Read only: no extraction, storage or import. Telegram files "+
			"have a 20 MiB read limit; larger archives require the operator upload AP"+
			"I.",
		map[string]any{
			schemaSourceFileID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaSHA256:       map[string]any{manifestType: schemaString, schemaPattern: schemaSHA256Pattern},
		},
		[]string{schemaSourceFileID, schemaSHA256},
		[]string{"archive", "inspection"},
	)
	method[manifestTimeout] = archiveValidationTimeout.Milliseconds()
	return method
}
