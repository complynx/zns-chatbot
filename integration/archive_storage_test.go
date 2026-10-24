package integration_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func prepareStoredArchive(t *testing.T, h *harness, admin string, op uuid.UUID,
	a db.LegacyArchiveArguments) uuid.UUID {
	t.Helper()
	code, data := adminRequest(t, h, admin, http.MethodPost, "/internal/agent/administration/store_archive/prepare",
		map[string]any{"protocol_version": 1, "operation_id": op, "arguments": a})
	require.Equal(t, http.StatusOK, code, string(data))
	var result struct {
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.NotEqual(t, uuid.Nil, result.Preconditions.ProposalID)
	return result.Preconditions.ProposalID
}

func storeArchiveBytes(t *testing.T, h *harness, admin string, r db.LegacyArchiveRequest, raw []byte) (int, []byte) {
	t.Helper()
	server := httptest.NewServer(h.endpoint.InternalHandler())
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/internal/admin/legacy/store", bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+admin)
	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("X-Content-Sha256", r.Arguments.SHA256)
	req.Header.Set("X-Operation-Id", r.OperationID.String())
	req.Header.Set("Idempotency-Key", r.OperationID.String())
	req.Header.Set("X-Proposal-Id", r.ProposalID.String())
	response, err := server.Client().Do(req)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, data
}

func storageRequest(t *testing.T, h *harness, admin, digest string) db.LegacyArchiveRequest {
	t.Helper()
	r := db.LegacyArchiveRequest{OperationID: uuid.New(), Arguments: db.LegacyArchiveArguments{SHA256: digest}}
	r.ProposalID = prepareStoredArchive(t, h, admin, r.OperationID, r.Arguments)
	return r
}

func TestStoredArchivePreservesExactChunksAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, admin := registrationUser(t, h, 9301, true)
	padding := make([]byte, 512<<10)
	_, err := rand.Read(padding)
	require.NoError(t, err)
	raw, digest := receivingArchiveData(t, 123, hex.EncodeToString(padding))
	r := storageRequest(t, h, admin, digest)
	code, data := storeArchiveBytes(t, h, admin, r, raw)
	require.Equal(t, http.StatusOK, code, string(data))
	var receipt db.LegacyArchiveReceipt
	require.NoError(t, json.Unmarshal(data, &receipt))
	require.False(t, receipt.Validation.Inspection.ImportPerformed)
	rows, err := h.store.Pool().
		Query(context.Background(), "SELECT ordinal,content FROM shuttle_legacy_archive_chunks WHERE archive_" +
"id=$1 ORDER BY ordinal", receipt.ArchiveID)
	require.NoError(t, err)
	var restored bytes.Buffer
	count := 0
	for rows.Next() {
		var ordinal int
		var chunk []byte
		require.NoError(t, rows.Scan(&ordinal, &chunk))
		require.Equal(t, count, ordinal)
		require.LessOrEqual(t, len(chunk), 256<<10)
		_, err = restored.Write(chunk)
		require.NoError(t, err)
		count++
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Greater(t, count, 1)
	require.Equal(t, raw, restored.Bytes())
	code, rechecked := agentAdminRead(
		t,
		h,
		admin,
		"validate_stored_archive",
		map[string]any{"archive_id": receipt.ArchiveID},
	)
	require.Equal(t, http.StatusOK, code, string(rechecked))
	var validated struct {
		Result legacyimport.BundleValidation `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rechecked, &validated))
	require.Equal(t, receipt.Validation, validated.Result)
	// A receipt survives expired approval and missing source bytes.
	_, err = h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_legacy_archive_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour' WHERE id=$1", r.ProposalID)
	require.NoError(t, err)
	code, replay := storeArchiveBytes(t, h, admin, r, nil)
	require.Equal(t, http.StatusOK, code, string(replay))
	require.JSONEq(t, string(data), string(replay))
	duplicate := storageRequest(t, h, admin, digest)
	code, dedup := storeArchiveBytes(t, h, admin, duplicate, nil)
	require.Equal(t, http.StatusOK, code, string(dedup))
	require.JSONEq(t, string(data), string(dedup))
	var archives, operations, users, tasks int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(context.Background(), "SELECT (SELECT count(*) FROM shuttle_legacy_archives),(SELECT count(*) F" +
"ROM shuttle_legacy_archive_operations),(SELECT count(*) FROM shuttle_use" +
"rs),(SELECT count(*) FROM shuttle_tasks)").
			Scan(&archives, &operations, &users, &tasks),
	)
	require.Equal(t, 1, archives)
	require.Equal(t, 2, operations)
	require.Equal(t, 1, users)
	require.Zero(t, tasks)
}

func TestArchiveStorageRejectsUnapprovedOrChangedOperations(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, ordinary := registrationUser(t, h, 9401, false)
	_, admin := registrationUser(t, h, 9402, true)
	raw, digest := receivingArchive(t, 123)
	r := storageRequest(t, h, admin, digest)
	code, _ := storeArchiveBytes(t, h, ordinary, r, raw)
	require.Equal(t, http.StatusForbidden, code)
	wrong := r
	wrong.ProposalID = uuid.New()
	code, _ = storeArchiveBytes(t, h, admin, wrong, raw)
	require.Equal(t, http.StatusConflict, code)
	code, data := storeArchiveBytes(t, h, admin, r, raw)
	require.Equal(t, http.StatusOK, code, string(data))
	wrong = r
	wrong.Arguments.SHA256 = hex.EncodeToString(make([]byte, sha256.Size))
	code, _ = storeArchiveBytes(t, h, admin, wrong, raw)
	require.Equal(t, http.StatusConflict, code)
}

func TestCorruptArchiveRollsBackAlreadyWrittenChunks(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, admin := registrationUser(t, h, 9501, true)
	padding := make([]byte, 512<<10)
	_, err := rand.Read(padding)
	require.NoError(t, err)
	raw, _ := receivingArchiveData(t, 123, hex.EncodeToString(padding))
	require.Greater(t, len(raw), 256<<10)
	raw[len(raw)-8] ^= 1 // Wrong CRC after multiple complete chunks.
	hash := sha256.Sum256(raw)
	r := storageRequest(t, h, admin, hex.EncodeToString(hash[:]))
	code, _ := storeArchiveBytes(t, h, admin, r, raw)
	require.Equal(t, http.StatusBadRequest, code)
	var archives, chunks, operations int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(context.Background(), "SELECT (SELECT count(*) FROM shuttle_legacy_archives),(SELECT count(*) F" +
"ROM shuttle_legacy_archive_chunks),(SELECT count(*) FROM shuttle_legacy_" +
"archive_operations)").
			Scan(&archives, &chunks, &operations),
	)
	require.Zero(t, archives)
	require.Zero(t, chunks)
	require.Zero(t, operations)
}

func TestArchiveStorageExpiredProposalCannotStoreArchive(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, admin := registrationUser(t, h, 9601, true)
	raw, digest := receivingArchive(t, 123)
	r := storageRequest(t, h, admin, digest)
	_, err := h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_legacy_archive_preparations SET expires_at=clock_timestam" +
"p()-interval '1 hour' WHERE id=$1", r.ProposalID)
	require.NoError(t, err)
	code, _ := storeArchiveBytes(t, h, admin, r, raw)
	require.Equal(t, http.StatusConflict, code)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var count int
	require.NoError(t, h.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_legacy_archives").Scan(&count))
	require.Zero(t, count)
}
