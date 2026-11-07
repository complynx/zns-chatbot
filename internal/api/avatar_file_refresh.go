package api

import (
	"context"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
)

const avatarFileLocatorMaxLength = 128

const avatarRefetchMethod = "refetch"
const historyAvatarRefetchMethod = "avatar_refetch"

type avatarRefetchArguments struct {
	ExternalID string `json:"external_id" minLength:"1" maxLength:"128"`
}

// File refresh carries only external_id. Locators retain the source kind and
// preview version; the existing readers still enforce current ownership.
func (s *Server) refetchAvatar(
	ctx context.Context,
	in *agentReadRequest[avatarRefetchArguments],
) (*fileEnvelope, error) {
	parts := strings.Split(in.Body.Arguments.ExternalID, ":")
	kind, locator := "", parts[0]
	if len(parts) > 1 {
		kind, locator = parts[0], parts[1]
	}
	id, err := canonicalAvatarUUID(locator)
	if err != nil {
		return nil, err
	}
	switch {
	case len(parts) == 1:
		return s.avatarFetch(ctx, avatarRefreshRequest(in, avatarArguments{ArtifactID: id}))
	case len(parts) == 2 && kind == "avatar-original":
		return s.avatarDraftFetch(ctx, avatarRefreshRequest(in, avatarDraftIDArguments{DraftID: id}))
	case len(parts) == 2 && kind == "avatar-finalized":
		return s.fetchFinalizedAvatar(ctx, avatarRefreshRequest(in, avatarArguments{ArtifactID: id}))
	case len(parts) == 3 && kind == "avatar-preview":
		version, parseErr := strconv.ParseInt(parts[2], 10, 64)
		if parseErr != nil || version < 1 || strconv.FormatInt(version, 10) != parts[2] {
			return nil, huma.Error400BadRequest("invalid avatar file locator")
		}
		return s.avatarDraftPreview(
			ctx,
			avatarRefreshRequest(in, avatarPreviewArguments{DraftID: id, ExpectedVersion: version}),
		)
	default:
		return nil, huma.Error400BadRequest("invalid avatar file locator")
	}
}

func (s *Server) refetchHistoryAvatar(
	ctx context.Context,
	in *agentReadRequest[avatarRefetchArguments],
) (*fileEnvelope, error) {
	archiveLocator, recordLocator, ok := strings.Cut(in.Body.Arguments.ExternalID, ":")
	if !ok {
		return nil, huma.Error400BadRequest("invalid historical image locator")
	}
	archive, err := canonicalAvatarUUID(archiveLocator)
	if err != nil {
		return nil, err
	}
	record, err := canonicalAvatarUUID(recordLocator)
	if err != nil {
		return nil, err
	}
	return s.historyAvatarFetch(
		ctx,
		avatarRefreshRequest(in, historyAvatarArguments{ArchiveID: archive, RecordID: record}),
	)
}

func canonicalAvatarUUID(value string) (uuid.UUID, error) {
	id, err := uuid.Parse(value)
	if err != nil || id == uuid.Nil || id.String() != value {
		return uuid.Nil, huma.Error400BadRequest("invalid avatar file locator")
	}
	return id, nil
}

func avatarRefreshRequest[T any](in *agentReadRequest[avatarRefetchArguments], args T) *agentReadRequest[T] {
	out := &agentReadRequest[T]{AuthInput: in.AuthInput}
	out.Body.ProtocolVersion,
		out.Body.OperationID,
		out.Body.RunID = in.Body.ProtocolVersion,
		in.Body.OperationID,
		in.Body.RunID
	out.Body.Arguments = args
	return out
}

func avatarRefetchManifest(history bool) map[string]any {
	id, path, timeout := avatarRefetchMethod, avatarPath, avatarUploadTimeout
	if history {
		id, path, timeout = historyAvatarRefetchMethod, historyPath, historyAvatarReadTimeout
	}
	method := administrativeRead(
		id,
		"Refresh a previously returned image descriptor using only its exact external_id. "+
			"Rechecks current owner and source availability; "+
			"a preview conflicts after the draft changes. Do not construct or alter locators. "+
			"If an original is unavailable, ask the person to resend it.",
		map[string]any{
			"external_id": map[string]any{
				manifestType:    schemaString,
				schemaMinLength: 1,
				schemaMaxLength: avatarFileLocatorMaxLength,
			},
		},
		[]string{"external_id"},
		nil,
	)
	method[manifestPath], method[manifestTimeout] = path+id, timeout.Milliseconds()
	method[manifestDisclosure], method[manifestMIMETypes] = true, []string{mimeJPEG, mimePNG}
	if history {
		method[manifestMIMETypes] = fileMIMETypes()
	}
	method[manifestOutputSchema] = map[string]any{manifestType: schemaObject, schemaAdditionalProperties: true}
	return method
}
