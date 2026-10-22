package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/avatar"
	"github.com/complynx/zns-chatbot/internal/db"
)

const (
	avatarPath                  = "/internal/agent/avatars/"
	avatarRenderTimeout         = 3 * time.Second
	avatarGenerateTimeoutMillis = 30000
	schemaArtifactID            = "artifact_id"
	schemaSourceFileID          = "source_file_id"
)

type avatarGenerateArguments struct {
	SourceFileID uuid.UUID         `json:"source_file_id"`
	Transform    *avatar.Transform `json:"transform,omitempty"`
}

type avatarArguments struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
}

type avatarResponse struct {
	Body struct {
		Status      string                  `json:"status"`
		OperationID string                  `json:"operation_id"`
		Result      *db.AvatarReceipt       `json:"result,omitempty"`
		Error       *agentRegistrationError `json:"error,omitempty"`
	}
}

func (s *Server) registerAvatars(api huma.API) {
	registerAgentPost(api, "agent-avatar-refetch", avatarPath+avatarRefetchMethod, internalBodyLimit, s.refetchAvatar)
	s.registerAvatarDrafts(api)
	s.registerAvatarFinalization(api)
	huma.Register(api, huma.Operation{OperationID: "agent-avatar-generate", Method: http.MethodPost,
		Path: avatarPath + "generate", MaxBodyBytes: internalBodyLimit}, s.generateAvatar)
	huma.Register(api, huma.Operation{OperationID: "agent-avatar-operation", Method: http.MethodPost,
		Path: avatarPath + "operation", MaxBodyBytes: internalBodyLimit}, s.avatarOperation)
	huma.Register(api, huma.Operation{OperationID: "agent-avatar-get", Method: http.MethodPost,
		Path: avatarPath + "get", MaxBodyBytes: internalBodyLimit}, s.avatarGet)
	huma.Register(api, huma.Operation{OperationID: "agent-avatar-fetch", Method: http.MethodPost,
		Path: avatarPath + "fetch", MaxBodyBytes: internalBodyLimit}, s.avatarFetch)
	s.internal.HandleFunc("GET /internal/avatars/{id}/content", s.avatarContent)
}

func (s *Server) generateAvatar(
	ctx context.Context,
	in *profileWriteRequest[avatarGenerateArguments],
) (*avatarResponse, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	op, err := uuid.Parse(in.Body.OperationID)
	if err != nil || op == uuid.Nil || in.Body.IdempotencyKey != in.Body.OperationID {
		return nil, huma.Error400BadRequest("operation ID must be a nonzero UUID and match idempotency key")
	}
	request := db.AvatarRequest{
		OperationID:  op,
		RunID:        in.Body.RunID,
		SourceFileID: in.Body.Arguments.SourceFileID,
		Transform:    in.Body.Arguments.Transform,
	}
	transform, err := avatar.NormalizeTransform(request.Transform)
	if err != nil {
		return avatarWriteResult(in.Body.OperationID, db.AvatarReceipt{}, err)
	}
	prior, err := s.store.AvatarPreflight(ctx, scope, request)
	if err != nil {
		return avatarWriteResult(in.Body.OperationID, db.AvatarReceipt{}, err)
	}
	if prior != nil {
		return avatarWriteResult(in.Body.OperationID, *prior, nil)
	}
	if s.options.AvatarRenderer == nil {
		return nil, huma.Error503ServiceUnavailable("avatar renderer unavailable")
	}
	record, err := s.ownedFile(ctx, in.AuthInput, request.SourceFileID)
	if err != nil {
		return nil, err
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		return nil, err
	}
	renderCtx, cancel := context.WithTimeout(ctx, avatarRenderTimeout)
	defer cancel()
	rendered, err := s.options.AvatarRenderer.Render(renderCtx, raw, transform)
	if err == nil {
		err = renderCtx.Err()
	}
	if err != nil {
		return avatarWriteResult(in.Body.OperationID, db.AvatarReceipt{}, err)
	}
	// Commit rechecks the current owner, source and active run after remote IO and rendering.
	result, err := s.store.CommitAvatar(ctx, scope, request, rendered)
	return avatarWriteResult(in.Body.OperationID, result, err)
}

func (s *Server) avatarOperation(ctx context.Context, in *profileOperationRequest) (*avatarResponse, error) {
	scope, op, err := s.ownerOperation(ctx, in)
	if err != nil {
		return nil, err
	}
	result, err := s.store.AvatarOperation(ctx, scope, op)
	if errors.Is(err, pgx.ErrNoRows) {
		out := &avatarResponse{}
		out.Body.Status, out.Body.OperationID = agentNotFound, in.Body.OperationID
		return out, nil
	}
	return avatarWriteResult(in.Body.OperationID, result, err)
}

func avatarWriteResult(operation string, result db.AvatarReceipt, err error) (*avatarResponse, error) {
	out := &avatarResponse{}
	out.Body.OperationID = operation
	if err != nil {
		if message := avatarRejection(err); message != "" {
			out.Body.Status = agentRejected
			out.Body.Error = &agentRegistrationError{Code: "avatar_rejected", Message: message,
				Classification: agentTerminalRejection}
			return out, nil
		}
		return nil, huma.Error503ServiceUnavailable("avatar processing unavailable")
	}
	out.Body.Status, out.Body.Result = agentSucceeded, &result
	return out, nil
}

func avatarRejection(err error) string {
	switch {
	case errors.Is(err, db.ErrAvatarSourceExpired):
		return db.ErrAvatarSourceExpired.Error()
	case errors.Is(err, errAvatarSourceUnavailable):
		return "source photo unavailable; send it again or upload another original"
	case errors.Is(err, avatar.ErrInvalidTransform):
		return "avatar transform is outside supported limits"
	case errors.Is(err, avatar.ErrInvalidImage):
		return "source must be a supported static image"
	case errors.Is(err, avatar.ErrImageLimit):
		return "source exceeds avatar image limits"
	case errors.Is(err, db.ErrAvatarQuota):
		return "avatar storage quota reached"
	case errors.Is(err, db.ErrInvalidAvatar):
		return "invalid avatar request"
	case errors.Is(err, db.ErrConflict):
		return "avatar operation conflicts with an earlier request"
	case errors.Is(err, pgx.ErrNoRows):
		return "source or active owned run unavailable"
	default:
		return ""
	}
}

func (s *Server) avatarGet(
	ctx context.Context,
	in *agentReadRequest[avatarArguments],
) (*agentReadResponse[db.AvatarView], error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	result, err := s.store.AvatarStatus(ctx, scope, in.Body.Arguments.ArtifactID)
	if err != nil {
		return nil, avatarReadError(err)
	}
	return agentReadResult(in.Body.OperationID, result), nil
}

func (s *Server) avatarFetch(ctx context.Context, in *agentReadRequest[avatarArguments]) (*fileEnvelope, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	view, err := s.store.AvatarStatus(ctx, scope, in.Body.Arguments.ArtifactID)
	if err != nil {
		return nil, avatarReadError(err)
	}
	result := struct {
		Status      string           `json:"status"`
		OperationID string           `json:"operation_id"`
		Result      db.AvatarReceipt `json:"result"`
		Artifacts   []fileDescriptor `json:"artifacts"`
	}{agentSucceeded, in.Body.OperationID, view.Avatar, []fileDescriptor{{
		ExternalID: view.Avatar.ArtifactID.String(), FetchMethod: avatarRefetchMethod,
		DownloadPath: "/internal/avatars/" + view.Avatar.ArtifactID.String() + "/content",
		MIMEType:     mimeJPEG, SizeBytes: view.Avatar.SizeBytes, SHA256: view.Avatar.SHA256,
	}}}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("avatar descriptor unavailable")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: body}, nil
}

func avatarReadError(err error) error {
	if errors.Is(err, db.ErrAvatarSourceExpired) {
		return huma.NewError(http.StatusGone, db.ErrAvatarSourceExpired.Error())
	}
	if errors.Is(err, errAvatarSourceUnavailable) {
		return huma.Error422UnprocessableEntity(errAvatarSourceUnavailable.Error())
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return huma.Error404NotFound("avatar unavailable")
	}
	return huma.Error503ServiceUnavailable("avatar storage unavailable")
}

func (s *Server) avatarContent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
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
	_, raw, err := s.store.GetAvatar(r.Context(), scope, id)
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
	w.Header().Set("Content-Type", mimeJPEG)
	w.Header().Set("Content-Disposition", `attachment; filename="zinusya-avatar.jpg"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(raw) // #nosec G705 -- owner-authorized JPEG download uses attachment, nosniff and sandbox CSP.
}

func avatarModule() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	input := func(field string) map[string]any {
		fields := map[string]any{}
		required := []string{}
		if field != "" {
			fields[field] = uuidField
			required = append(required, field)
		}
		return map[string]any{
			manifestType:               schemaObject,
			schemaAdditionalProperties: false,
			schemaRequired:             required,
			schemaProperties:           fields,
		}
	}
	method := func(id, description string, args, output map[string]any) map[string]any {
		return map[string]any{"id": id, manifestDescription: description,
			manifestPermissions: []string{}, manifestPath: avatarPath + id, manifestEffect: effectRead,
			manifestTimeout: toolTimeoutMillis, manifestMaxResult: toolResultLimit,
			manifestInputSchema: args, manifestOutputSchema: output, manifestIdempotency: false,
			manifestConfirmation: map[string]any{confirmationMode: confirmationNone},
			manifestMIMETypes:    []string{}, manifestDisclosure: false}
	}
	generate := method(
		"generate",
		"After an explicit avatar request, create a framed 1024-square JPEG from your source_file_id. Optional transform uses frame-relative PPM pan/zoom and clockwise millidegree rotation after EXIF orientation and centered cover fit; 1000000 PPM is one frame/1x zoom. Omitted or explicit identity keeps legacy centered output. Exposed background is white. This creates a separate downloadable document, not a Telegram profile photo. Supply the current owned active run_id in the envelope. The source attachment reference is kept; original bytes remain in Telegram. Success means the document is durably queued for your current chat, not delivered. Reconcile uncertainty with operation; reuse the same operation ID, source, run and transform. Never generate an avatar merely because a photo was uploaded, and never classify payment proof as an avatar from menu state.",
		avatarGenerateSchema(),
		avatarReceiptSchema(),
	)
	generate[manifestEffect], generate[manifestIdempotency] = effectWrite, true
	generate[manifestTimeout] = avatarGenerateTimeoutMillis
	generate[manifestStatusPath], generate[manifestDedupWindow] = avatarPath+"operation", agentRegistrationDedupSeconds
	get := method(
		"get",
		"Read your generated avatar and its current delivery state. Receipt provenance is immutable; delivery state is current and may be pending, delivered or uncertain. No administrator bypass exists.",
		input(schemaArtifactID),
		avatarViewSchema(),
	)
	fetch := method(
		"fetch",
		"Explicitly import your generated JPEG into Loom's file tools by artifact_id. Content remains in Zinusya and requires current owner authorization.",
		input(schemaArtifactID),
		avatarReceiptSchema(),
	)
	fetch[manifestDisclosure], fetch[manifestMIMETypes] = true, []string{mimeJPEG}
	operation := method(
		"operation",
		"Read the original immutable generation receipt using arguments.operation_id. The status endpoint also accepts protocol_version and operation_id alone in the envelope. A missing receipt is unknown; do not invent a new operation after an uncertain result. This receipt does not assert document delivery.",
		input("operation_id"),
		avatarReceiptSchema(),
	)
	return map[string]any{
		"id":                "zinusya_avatars",
		manifestDescription: "Owner-scoped framed avatar documents",
		manifestPermissions: []string{},
		manifestDiscovery:   discoveryStatic,
		manifestMethods: append(
			append([]any{generate, get, fetch, operation, avatarRefetchManifest(false)}, avatarDraftMethods()...),
			avatarFinalizationMethods(method, input)...),
	}
}

func avatarReceiptSchema() map[string]any {
	uuidField := map[string]any{manifestType: schemaString, schemaFormat: schemaUUID}
	digest := map[string]any{manifestType: schemaString, schemaPattern: "^[a-f0-9]{64}$"}
	positive := map[string]any{manifestType: schemaInteger, schemaMinimum: 1}
	return map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{
			schemaArtifactID,
			schemaTaskID,
			"delivery_id",
			schemaSourceFileID,
			"source_sha256",
			"frame_sha256",
			"processor_version",
			schemaSHA256,
			"width",
			"height",
			schemaSizeBytes,
			schemaCreatedAt,
		},
		schemaProperties: map[string]any{
			schemaArtifactID:    uuidField,
			schemaTaskID:        uuidField,
			"delivery_id":       uuidField,
			schemaSourceFileID:  uuidField,
			"source_sha256":     digest,
			"frame_sha256":      digest,
			"sha256":            digest,
			"processor_version": map[string]any{manifestType: schemaString, schemaMinLength: 1},
			"transform":         avatarTransformSchema(),
			"width":             positive,
			"height":            positive,
			schemaSizeBytes:     positive,
			schemaCreatedAt:     map[string]any{manifestType: schemaString, schemaFormat: schemaDateTime},
		}}
}

func avatarViewSchema() map[string]any {
	return map[string]any{manifestType: schemaObject, schemaAdditionalProperties: false,
		schemaRequired: []string{"avatar", "delivery_state"}, schemaProperties: map[string]any{
			"avatar": avatarReceiptSchema(), "delivery_state": map[string]any{manifestType: schemaString},
		}}
}
