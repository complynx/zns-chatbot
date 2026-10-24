package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
)

func (s *Server) registerPaymentFiles(api huma.API) {
	huma.Register(
		api,
		huma.Operation{
			OperationID:  "agent-payment-fetch",
			Method:       http.MethodPost,
			Path:         paymentPath + fileFetchMethod,
			MaxBodyBytes: internalBodyLimit,
		},
		s.paymentFileFetch,
	)
	s.internal.HandleFunc("GET /internal/payment-evidence/{id}/content", s.paymentFileContent)
}
func (s *Server) authorizedPaymentFile(ctx context.Context, auth AuthInput, id uuid.UUID) (db.File, error) {
	scope, err := s.paymentScope(ctx, auth)
	if err != nil {
		return db.File{}, err
	}
	record, err := s.store.PaymentFile(ctx, scope, id)
	if err != nil {
		return db.File{}, paymentError(err)
	}
	return record, nil
}
func (s *Server) paymentFileFetch(ctx context.Context, in *agentReadRequest[fileArguments]) (*fileEnvelope, error) {
	id := in.Body.Arguments.ID
	record, err := s.authorizedPaymentFile(ctx, in.AuthInput, id)
	if err != nil {
		return nil, err
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		return nil, err
	}
	// Event-role revocation during Telegram IO must prevent releasing content authority.
	if _, err = s.authorizedPaymentFile(ctx, in.AuthInput, id); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	result := struct {
		Status      string           `json:"status"`
		OperationID string           `json:"operation_id"`
		Result      files.Metadata   `json:"result"`
		Artifacts   []fileDescriptor `json:"artifacts"`
	}{agentSucceeded,
		in.Body.OperationID,
		record.Metadata,
		[]fileDescriptor{{ExternalID: id.String(),
			FetchMethod: fileFetchMethod,
			DownloadPath: "/internal/payment-evidence/" +
				id.String() +
				"/content",
			MIMEType:  fileContentType(raw),
			SizeBytes: int64(len(raw)),
			SHA256:    hex.EncodeToString(digest[:])}}}
	body, err := json.Marshal(result)
	if err != nil {
		return nil, huma.Error503ServiceUnavailable("proof descriptor unavailable")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: body}, nil
}
func (s *Server) paymentFileContent(w http.ResponseWriter, r *http.Request) {
	s.serveFileContent(w, r, s.authorizedPaymentFile)
}
