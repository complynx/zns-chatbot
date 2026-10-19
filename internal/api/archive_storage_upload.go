package api

import (
	"context"
	"io"
	"mime"
	"net/http"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func (s *Server) storeArchiveUpload(w http.ResponseWriter, r *http.Request) {
	auth := AuthInput{Authorization: r.Header.Get("Authorization")}
	scope, err := s.archiveAdminScope(r.Context(), auth)
	if err != nil {
		fileHTTPError(w, err)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/gzip" || len(params) != 0 {
		http.Error(w, "application/gzip required", http.StatusUnsupportedMediaType)
		return
	}
	op, opErr := uuid.Parse(r.Header.Get("X-Operation-Id"))
	proposal, proposalErr := uuid.Parse(r.Header.Get("X-Proposal-Id"))
	if opErr != nil || proposalErr != nil || op == uuid.Nil || proposal == uuid.Nil ||
		r.Header.Get("Idempotency-Key") != op.String() {
		http.Error(w, "matching operation, idempotency key and proposal UUID required", http.StatusBadRequest)
		return
	}
	if err = s.beginArchiveValidation(); err != nil {
		fileHTTPError(w, err)
		return
	}
	defer s.endArchiveValidation()
	body := http.MaxBytesReader(w, r.Body, legacyimport.MaxBundleBytes)
	defer func() { _ = body.Close() }()
	receipt, err := s.store.StoreLegacyArchive(
		r.Context(),
		scope,
		db.LegacyArchiveRequest{OperationID: op,
			ProposalID: proposal, Arguments: db.LegacyArchiveArguments{SHA256: r.Header.Get("X-Content-Sha256")}},
		func(context.Context) (io.ReadCloser, error) { return io.NopCloser(body), nil },
		s.archiveIdentityCheck(auth.Authorization),
	)
	if err != nil {
		fileHTTPError(w, legacyArchiveError(err))
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, receipt)
}

func archiveStorageMethod() map[string]any {
	method := administrativeRead(
		"store_archive",
		"Store an exact approved caller-owned legacy archive durably. Requires source_file_id and SHA-256. No live users, history or payments are applied. Reconcile unknown outcomes using the same operation ID; never upload again with a fresh operation to resolve uncertainty.",
		map[string]any{
			schemaSourceFileID: map[string]any{manifestType: schemaString, schemaFormat: schemaUUID},
			schemaSHA256:       map[string]any{manifestType: schemaString, schemaPattern: schemaSHA256Pattern},
		},
		[]string{schemaSourceFileID, schemaSHA256},
		[]string{schemaArchiveID, "stored_at", "validation"},
	)
	method[manifestEffect], method[manifestIdempotency] = effectWrite, true
	method[manifestTimeout] = archiveValidationTimeout.Milliseconds()
	method[manifestStatusPath] = "/internal/agent/administration/store_archive/status"
	method[manifestDedupWindow] = agentRegistrationDedupSeconds
	method[manifestConfirmation] = map[string]any{confirmationMode: assignmentConfirmationMode,
		manifestPreparePath: "/internal/agent/administration/store_archive/prepare"}
	return method
}
