package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/files"
)

func acceptedArchiveFile(t *testing.T, h *harness) uuid.UUID {
	t.Helper()
	taskID := submitFile(t, h, 98201, time.Now().UTC())
	task, err := h.store.GetTask(context.Background(), taskID)
	require.NoError(t, err)
	var intent struct {
		Attachments []files.Incoming `json:"attachments"`
	}
	require.NoError(t, json.Unmarshal(task.Request, &intent))
	require.Len(t, intent.Attachments, 1)
	return intent.Attachments[0].ID
}

func storeArchiveFile(t *testing.T, h *harness, token string, r db.LegacyArchiveRequest) (int, []byte) {
	t.Helper()
	return adminRequest(t, h, token, http.MethodPost, "/internal/agent/administration/store_archive", map[string]any{
		"protocol_version": 1,
		"operation_id":     r.OperationID,
		"idempotency_key":  r.OperationID,
		"arguments":        r.Arguments,
		"confirmation": map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": r.ProposalID},
		},
	})
}

func archiveFileReceipt(t *testing.T, raw []byte, operation uuid.UUID) db.LegacyArchiveReceipt {
	t.Helper()
	var response struct {
		Status      string                  `json:"status"`
		OperationID uuid.UUID               `json:"operation_id"`
		Result      db.LegacyArchiveReceipt `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Equal(t, "succeeded", response.Status)
	require.Equal(t, operation, response.OperationID)
	return response.Result
}

func TestStoredArchiveFileReceiptWorksAfterTelegramFileDisappears(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	raw, digest := receivingArchive(t, 123)
	p.fileBytes = raw
	h := setup(t, p)
	_, admin := registrationUser(t, h, 456, true)
	r := db.LegacyArchiveRequest{
		OperationID: uuid.New(),
		Arguments:   db.LegacyArchiveArguments{SHA256: digest, SourceFileID: acceptedArchiveFile(t, h)},
	}
	r.ProposalID = prepareStoredArchive(t, h, admin, r.OperationID, r.Arguments)
	code, data := storeArchiveFile(t, h, admin, r)
	require.Equal(t, http.StatusOK, code, string(data))
	p.mu.Lock()
	reads := p.fileGets
	p.fileGone = true
	p.mu.Unlock()
	code, replay := storeArchiveFile(t, h, admin, r)
	require.Equal(t, http.StatusOK, code, string(replay))
	require.Equal(t, archiveFileReceipt(t, data, r.OperationID), archiveFileReceipt(t, replay, r.OperationID))
	p.mu.Lock()
	require.Equal(t, reads, p.fileGets)
	p.mu.Unlock()
	code, status := adminRequest(t, h, admin, http.MethodPost, "/internal/agent/administration/store_archive/status",
		map[string]any{"protocol_version": 1, "operation_id": r.OperationID})
	require.Equal(t, http.StatusOK, code, string(status))
	require.Equal(t, archiveFileReceipt(t, data, r.OperationID), archiveFileReceipt(t, status, r.OperationID))
}

func TestArchiveFileStorageRevocationDuringReadRollsBack(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	raw, digest := receivingArchive(t, 123)
	p.fileBytes = raw
	h := setup(t, p)
	ownerID, admin := registrationUser(t, h, 456, true)
	_, otherAdmin := registrationUser(t, h, 9702, true)
	fileID := acceptedArchiveFile(t, h)
	r := db.LegacyArchiveRequest{
		OperationID: uuid.New(),
		Arguments:   db.LegacyArchiveArguments{SHA256: digest, SourceFileID: fileID},
	}
	code, _ := adminRequest(t, h, otherAdmin, http.MethodPost, "/internal/agent/administration/store_archive/prepare",
		map[string]any{"protocol_version": 1, "operation_id": r.OperationID, "arguments": r.Arguments})
	require.Equal(t, http.StatusNotFound, code)
	r.ProposalID = prepareStoredArchive(t, h, admin, r.OperationID, r.Arguments)
	done := make(chan error, 1)
	p.mu.Lock()
	p.fileReadHook = func() { _, err := h.store.SetUserRevoked(context.Background(), ownerID, "fixture"); done <- err }
	p.mu.Unlock()
	code, data := storeArchiveFile(t, h, admin, r)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	require.Contains(t, string(data), `"classification":"terminal_rejection"`)
	require.NoError(t, <-done)
	var archives, operations, chunks int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(context.Background(), "SELECT (SELECT count(*) FROM shuttle_legacy_archives),(SELECT count(*) F" +
"ROM shuttle_legacy_archive_operations),(SELECT count(*) FROM shuttle_leg" +
"acy_archive_chunks)").
			Scan(&archives, &operations, &chunks),
	)
	require.Zero(t, archives)
	require.Zero(t, operations)
	require.Zero(t, chunks)
}
