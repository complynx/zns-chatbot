package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/legacyimport"
)

func TestStoredArchiveRevalidationChecksActualBytesAndCurrentAdmin(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	ownerID, admin := registrationUser(t, h, 9901, true)
	_, ordinary := registrationUser(t, h, 9902, false)
	raw, digest := receivingArchive(t, 123)
	r := storageRequest(t, h, admin, digest)
	code, data := storeArchiveBytes(t, h, admin, r, raw)
	require.Equal(t, http.StatusOK, code, string(data))
	var stored db.LegacyArchiveReceipt
	require.NoError(t, json.Unmarshal(data, &stored))
	args := map[string]any{"archive_id": stored.ArchiveID}
	code, _ = agentAdminRead(t, h, ordinary, "validate_stored_archive", args)
	require.Equal(t, http.StatusForbidden, code)
	// Recompute the archive result even if the saved report is no longer usable.
	_, err := h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_legacy_archives SET validation='{}' WHERE id=$1", stored.ArchiveID)
	require.NoError(t, err)
	code, data = agentAdminRead(t, h, admin, "validate_stored_archive", args)
	require.Equal(t, http.StatusOK, code, string(data))
	var response struct {
		Result legacyimport.BundleValidation `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &response))
	require.Equal(t, stored.Validation, response.Result)
	require.NotContains(t, string(data), "PRIVATE_ARCHIVE_NAME")
	foreign := uuid.New()
	_, err = h.store.Pool().
		Exec(context.Background(), "INSERT INTO shuttle_legacy_archives(id,bot_id,organization_id,sha256,siz" +
"e_bytes,validation,created_by) VALUES($1,124,'org',$2,$3,'{}',$4)", foreign, digest, len(raw), ownerID)
	require.NoError(t, err)
	code, _ = agentAdminRead(t, h, admin, "validate_stored_archive", map[string]any{"archive_id": foreign})
	require.Equal(t, http.StatusNotFound, code)
	bad := append([]byte(nil), raw...)
	bad[len(bad)-8] ^= 1
	_, err = h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_legacy_archive_chunks SET content=$1 WHERE archive_id=$2 " +
"AND ordinal=0", bad, stored.ArchiveID)
	require.NoError(t, err)
	code, _ = agentAdminRead(t, h, admin, "validate_stored_archive", args)
	require.Equal(t, http.StatusBadRequest, code)
	_, err = h.store.Pool().
		Exec(context.Background(), "DELETE FROM shuttle_legacy_archive_chunks WHERE archive_id=$1", stored.ArchiveID)
	require.NoError(t, err)
	code, _ = agentAdminRead(t, h, admin, "validate_stored_archive", args)
	require.Equal(t, http.StatusBadRequest, code)
}
