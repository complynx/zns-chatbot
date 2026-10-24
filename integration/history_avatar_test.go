package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

type historyAvatarFixture struct {
	h         *harness
	admin     db.ProfileScope
	recordID  uuid.UUID
	archiveID uuid.UUID
}

func historyAvatarSetup(t *testing.T) historyAvatarFixture {
	t.Helper()
	h := setup(t, newPeers(t))
	id, _ := registrationUser(t, h, 700, true)
	scope := historyScope(t, h, id)
	archive, digest := historyBundle(t, "legacy-avatar-reference", map[string][]map[string]any{
		"users": {historyUser(42)},
		"files": {{"_id": "source", "bot_id": 123, "user_id": 42, "mime_type": "image/png",
			"tg_file_id": "PRIVATE_OLD_FILE", "tg_file_unique_id": "PRIVATE_OLD_UNIQUE", "status": "completed_simple"}},
	})
	saved := saveHistoryBundle(t, h.store, scope, archive, digest)
	_, err := h.store.ApplyHistory(t.Context(), scope, prepareHistory(t, h.store, scope, saved),
		func(context.Context) error { return nil })
	require.NoError(t, err)
	var recordID uuid.UUID
	require.NoError(t, h.store.Pool().QueryRow(t.Context(),
		`SELECT id FROM shuttle_legacy_history_records WHERE source_role='files'`).Scan(&recordID))
	return historyAvatarFixture{h, scope, recordID, saved.ArchiveID}
}

func TestHistoryAvatarReferenceUsesCurrentOwnerWithoutCreatingLiveState(t *testing.T) {
	t.Parallel()
	f := historyAvatarSetup(t)
	check := func(context.Context) error { return nil }
	_, err := f.h.store.OwnerHistoryAvatarSource(t.Context(), f.admin, f.recordID, f.archiveID, check)
	require.ErrorIs(t, err, pgx.ErrNoRows, "admin rights do not bypass historical file ownership")
	id, _ := registrationUser(t, f.h, 42, false)
	scope := historyScope(t, f.h, id)
	result, err := f.h.store.OwnerHistoryAvatarSource(t.Context(), scope, f.recordID, f.archiveID, check)
	require.NoError(t, err)
	require.Equal(t, "PRIVATE_OLD_FILE", result.FileID)
	require.Equal(t, "PRIVATE_OLD_UNIQUE", result.UniqueID)
	require.Equal(t, "image/png", result.MIMEType)
	require.Equal(t, f.archiveID, result.ArchiveID)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "PRIVATE_")
	var tasks, drafts, outbox int
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(),
		"SELECT (SELECT count(*) FROM shuttle_tasks),(SELECT count(*) FROM shuttl" +
"e_avatar_drafts),(SELECT count(*) FROM shuttle_outbox)").
		Scan(&tasks, &drafts, &outbox))
	require.Zero(t, tasks)
	require.Zero(t, drafts)
	require.Zero(t, outbox)
	_, err = f.h.store.Pool().Exec(t.Context(), `UPDATE shuttle_users SET tg_user_id=44 WHERE id=$1`, id)
	require.NoError(t, err)
	_, err = f.h.store.OwnerHistoryAvatarSource(t.Context(), scope, f.recordID, f.archiveID, check)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestHistoryAvatarReferenceRechecksIAMBeforeReturningPrivateIDs(t *testing.T) {
	t.Parallel()
	f := historyAvatarSetup(t)
	id, _ := registrationUser(t, f.h, 42, false)
	denied := errors.New("current IAM authority revoked")
	result, err := f.h.store.OwnerHistoryAvatarSource(t.Context(), historyScope(t, f.h, id), f.recordID, f.archiveID,
		func(context.Context) error { return denied })
	require.ErrorIs(t, err, denied)
	require.Empty(t, result.FileID)
	require.Empty(t, result.UniqueID)
}

func TestHistoryAvatarReferenceRejectsDifferentOwnerAndArchive(t *testing.T) {
	t.Parallel()
	f := historyAvatarSetup(t)
	id, _ := registrationUser(t, f.h, 43, false)
	check := func(context.Context) error { return nil }
	result, err := f.h.store.OwnerHistoryAvatarSource(
		t.Context(),
		historyScope(t, f.h, id),
		f.recordID,
		f.archiveID,
		check,
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, result.FileID)
	ownerID, _ := registrationUser(t, f.h, 42, false)
	result, err = f.h.store.OwnerHistoryAvatarSource(
		t.Context(),
		historyScope(t, f.h, ownerID),
		f.recordID,
		uuid.New(),
		check,
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Empty(t, result.FileID)
}
