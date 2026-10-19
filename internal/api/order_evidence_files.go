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

const orderEvidenceContentPath = "/internal/order-evidence/"
const orderEvidenceReviewFetch = "fetch_for_review"

func (s *Server) registerOrderEvidenceFiles(api huma.API) {
	huma.Register(api, orderEvidenceRoute(fileFetchMethod), s.agentOwnOrderEvidenceFetch)
	huma.Register(api, orderEvidenceRoute(orderEvidenceReviewFetch), s.agentReviewOrderEvidenceFetch)
	s.internal.HandleFunc(
		"GET /internal/order-evidence/{id}/content",
		func(w http.ResponseWriter, r *http.Request) { s.orderEvidenceContent(w, r, false) },
	)
	s.internal.HandleFunc(
		"GET /internal/order-evidence/{id}/review-content",
		func(w http.ResponseWriter, r *http.Request) { s.orderEvidenceContent(w, r, true) },
	)
}

func (s *Server) authorizedOrderEvidenceFile(
	ctx context.Context,
	auth AuthInput,
	id uuid.UUID,
	audit bool,
) (db.File, error) {
	scope, err := s.paymentScope(ctx, auth)
	if err != nil {
		return db.File{}, err
	}
	record, err := s.store.OrderEvidenceFile(ctx, scope, id, audit)
	if err != nil {
		return db.File{}, orderEvidenceError(err)
	}
	return record, nil
}

func (s *Server) agentOwnOrderEvidenceFetch(
	ctx context.Context,
	in *agentReadRequest[fileArguments],
) (*fileEnvelope, error) {
	return s.agentOrderEvidenceFetch(ctx, in, false)
}

func (s *Server) agentReviewOrderEvidenceFetch(
	ctx context.Context,
	in *agentReadRequest[fileArguments],
) (*fileEnvelope, error) {
	return s.agentOrderEvidenceFetch(ctx, in, true)
}

func (s *Server) agentOrderEvidenceFetch(
	ctx context.Context,
	in *agentReadRequest[fileArguments],
	audit bool,
) (*fileEnvelope, error) {
	id := in.Body.Arguments.ID
	record, err := s.authorizedOrderEvidenceFile(ctx, in.AuthInput, id, audit)
	if err != nil {
		return nil, err
	}
	raw, err := s.readFile(ctx, record)
	if err != nil {
		return nil, err
	}
	current, err := s.authorizedOrderEvidenceFile(ctx, in.AuthInput, id, audit)
	if err != nil {
		return nil, err
	}
	if current.ID != record.ID || current.TelegramID != record.TelegramID {
		return nil, huma.Error409Conflict("order evidence file changed during read")
	}
	method, suffix := fileFetchMethod, "/content"
	if audit {
		method, suffix = orderEvidenceReviewFetch, "/review-content"
	}
	digest := sha256.Sum256(raw)
	result := struct {
		Status      string           `json:"status"`
		OperationID string           `json:"operation_id"`
		Result      files.Metadata   `json:"result"`
		Artifacts   []fileDescriptor `json:"artifacts"`
	}{agentSucceeded, in.Body.OperationID, record.Metadata, []fileDescriptor{{ExternalID: id.String(), FetchMethod: method, DownloadPath: orderEvidenceContentPath + id.String() + suffix, MIMEType: fileContentType(raw), SizeBytes: int64(len(raw)), SHA256: hex.EncodeToString(digest[:])}}}
	body, err := json.Marshal(result)
	if err != nil || len(body) > agentAdminResultLimit {
		return nil, huma.Error503ServiceUnavailable("order evidence descriptor exceeds supported bound")
	}
	return &fileEnvelope{ContentType: contentTypeJSON, Body: body}, nil
}

func (s *Server) orderEvidenceContent(w http.ResponseWriter, r *http.Request, audit bool) {
	var first *db.File
	s.serveFileContent(w, r, func(ctx context.Context, auth AuthInput, id uuid.UUID) (db.File, error) {
		record, err := s.authorizedOrderEvidenceFile(ctx, auth, id, audit)
		if err != nil {
			return db.File{}, err
		}
		if first != nil && (first.ID != record.ID || first.TelegramID != record.TelegramID) {
			return db.File{}, huma.Error409Conflict("order evidence file changed during read")
		}
		first = &record
		return record, nil
	})
}
