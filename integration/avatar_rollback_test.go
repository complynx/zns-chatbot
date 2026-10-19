package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

func avatarLegacyRollbackSQL(t *testing.T) (string, string) {
	t.Helper()
	raw, err := migrations.Files.ReadFile("00020_avatars.sql")
	require.NoError(t, err)
	up, down, ok := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, ok)
	// Exercise the historical avatar migration beneath the current extension.
	extensionUp, extensionDown := avatarFinalizationMigration(t)
	return up + "\n" + extensionUp, extensionDown + "\n" + down
}
func TestAvatarLegacyRollbackAllowsEmptyAndRejectsCommittedHistory(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	f.h.stop(t)
	up, down := avatarLegacyRollbackSQL(t)
	_, err := f.h.store.Pool().Exec(t.Context(), down)
	require.NoError(t, err)
	_, err = f.h.store.Pool().Exec(t.Context(), up)
	require.NoError(t, err)
	scope := ownerProfileScope(t, f.h, f.ownerID)
	a := db.AvatarRequest{OperationID: uuid.New(), RunID: f.runID, SourceFileID: f.source}
	receipt, err := f.h.store.CommitAvatar(t.Context(), scope, a, f.rendered)
	require.NoError(t, err)
	_, err = f.h.store.Pool().Exec(t.Context(), down)
	require.ErrorContains(t, err, "avatar bytes and operation history must be exported before rollback")
	current, original, err := f.h.store.GetAvatar(t.Context(), scope, receipt.ArtifactID)
	require.NoError(t, err)
	require.Equal(t, receipt, current)
	require.Equal(t, f.rendered.JPEG, original)
	replay, err := f.h.store.AvatarPreflight(t.Context(), scope, a)
	require.NoError(t, err)
	require.Equal(t, &receipt, replay)
}
func TestAvatarLegacyRollbackRejectsUncommittedArtifact(t *testing.T) {
	t.Parallel()
	f := setupAvatar(t, nil)
	f.h.stop(t)
	_, down := avatarLegacyRollbackSQL(t)
	scope := ownerProfileScope(t, f.h, f.ownerID)
	writer, err := f.h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = writer.Rollback(t.Context()) }()
	artifact, delivery, operation := uuid.New(), uuid.New(), uuid.New()
	a := db.AvatarRequest{OperationID: operation, RunID: f.runID, SourceFileID: f.source}
	encoded, err := json.Marshal(a)
	require.NoError(t, err)
	hash := sha256.Sum256(encoded)
	receipt := db.AvatarReceipt{
		ArtifactID:       artifact,
		TaskID:           f.taskID,
		DeliveryID:       delivery,
		SourceFileID:     f.source,
		SourceSHA256:     f.rendered.SourceSHA256,
		FrameSHA256:      f.rendered.FrameSHA256,
		ProcessorVersion: f.rendered.ProcessorVersion,
		SHA256:           f.rendered.SHA256,
		Width:            f.rendered.Width,
		Height:           f.rendered.Height,
		SizeBytes:        int64(len(f.rendered.JPEG)),
		CreatedAt:        time.Now().UTC(),
	}
	rawReceipt, err := json.Marshal(receipt)
	require.NoError(t, err)
	_, err = writer.Exec(
		t.Context(),
		`INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload) SELECT $1,$2,$3,coalesce(max(chunk_seq),0)+1,'{}'::jsonb FROM shuttle_outbox WHERE task_id=$2`,
		delivery,
		f.taskID,
		"legacy-rollback-fixture:"+operation.String(),
	)
	require.NoError(t, err)
	_, err = writer.Exec(
		t.Context(),
		`INSERT INTO shuttle_avatars(id,user_id,task_id,delivery_id,source_file_id,operation_id,request_hash,receipt,jpeg,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		artifact,
		f.ownerID,
		f.taskID,
		delivery,
		f.source,
		operation,
		hash[:],
		rawReceipt,
		f.rendered.JPEG,
		receipt.CreatedAt,
	)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err = f.h.store.Pool().Exec(ctx, down)
	var lockError *pgconn.PgError
	require.ErrorAs(t, err, &lockError)
	require.Equal(t, "55P03", lockError.Code, "Rollback must refuse an active writer before any destructive statement")
	require.NoError(t, writer.Commit(t.Context()))
	current, original, err := f.h.store.GetAvatar(t.Context(), scope, artifact)
	require.NoError(t, err)
	require.Equal(t, receipt, current)
	require.Equal(t, f.rendered.JPEG, original)
	replay, err := f.h.store.AvatarPreflight(t.Context(), scope, a)
	require.NoError(t, err)
	require.Equal(t, &receipt, replay)
}
