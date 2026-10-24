package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
)

func (s *Server) avatarDraftFetch(
	ctx context.Context,
	in *agentReadRequest[avatarDraftIDArguments],
) (*fileEnvelope, error) {
	scope, err := s.profileScope(ctx, in.AuthInput)
	if err != nil {
		return nil, err
	}
	d, err := s.store.GetAvatarDraft(ctx, scope, in.Body.Arguments.DraftID)
	if err != nil {
		return nil, avatarReadError(err)
	}
	if d.SourceExpired {
		return nil, avatarReadError(db.ErrAvatarSourceExpired)
	}
	result := struct {
		Status      string           `json:"status"`
		OperationID string           `json:"operation_id"`
		Result      db.AvatarDraft   `json:"result"`
		Artifacts   []fileDescriptor `json:"artifacts"`
	}{agentSucceeded,
		in.Body.OperationID,
		d,
		[]fileDescriptor{{ExternalID: "avatar-original:" + d.ID.String(),
			FetchMethod: avatarRefetchMethod,
			DownloadPath: "/internal/avatar-drafts/" +
				d.ID.String() +
				"/original",
			MIMEType:  d.MIMEType,
			SizeBytes: d.SizeBytes,
			SHA256:    d.SourceSHA256}}}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("avatar original descriptor unavailable")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: raw}, nil
}
func (s *Server) avatarDraftContent(w http.ResponseWriter, r *http.Request) {
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
	d, raw, err := s.readAvatarDraftOriginal(r.Context(), scope, id)
	if err != nil {
		if !writeAvatarSourceUnavailable(w, err) {
			fileHTTPError(w, avatarReadError(err))
		}
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
	writeAvatarOriginal(w, d, raw)
}
func writeAvatarOriginal(w http.ResponseWriter, d db.AvatarDraft, raw []byte) {
	w.Header().Set("Content-Type", d.MIMEType)
	w.Header().Set("Content-Disposition", `attachment; filename="avatar-original"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(raw) // #nosec G705 -- validated static image download with attachment, nosniff and sandbox CSP.
}
