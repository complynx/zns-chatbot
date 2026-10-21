package db

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func avatarDraftOperation(
	ctx context.Context,
	tx pgx.Tx,
	owner, operation uuid.UUID,
	hash []byte,
) (*AvatarDraft, error) {
	var saved, raw []byte
	err := tx.QueryRow(ctx, `SELECT request_hash,result FROM shuttle_avatar_draft_operations WHERE user_id=$1 AND operation_id=$2`, owner, operation).
		Scan(&saved, &raw)
	if err != nil {
		return nil, err
	}
	if hash != nil && !bytes.Equal(hash, saved) {
		return nil, ErrConflict
	}
	var result AvatarDraft
	err = json.Unmarshal(raw, &result)
	return &result, err
}

func readAvatarDraft(ctx context.Context, tx pgx.Tx, owner, id uuid.UUID, content bool) (AvatarDraft, []byte, error) {
	var d AvatarDraft
	policy, now, err := avatarSourcePolicy(ctx, tx)
	if err != nil {
		return d, nil, err
	}
	var expired pgtype.Timestamptz
	var source, historyRecord, historyArchive pgtype.UUID
	var digest, frame, transform, original []byte
	err = tx.QueryRow(ctx, `SELECT id,source_file_id,source_sha256,mime_type,source_size_bytes,frame_sha256,transform,version,created_at,updated_at,CASE WHEN $3 AND original_expired_at IS NULL AND created_at > $4 THEN original ELSE NULL END,history_record_id,history_archive_id,original_expired_at FROM shuttle_avatar_drafts WHERE user_id=$1 AND id=$2`, owner, id, content, now.Add(-policy.Retention)).
		Scan(&d.ID, &source, &digest, &d.MIMEType, &d.SizeBytes, &frame, &transform, &d.Version, &d.CreatedAt, &d.UpdatedAt, &original, &historyRecord, &historyArchive, &expired)
	if err != nil {
		return d, nil, err
	}
	if source.Valid {
		sourceID := uuid.UUID(source.Bytes)
		d.SourceFileID = &sourceID
	}
	if historyRecord.Valid && historyArchive.Valid {
		d.HistorySource = &AvatarHistorySource{
			RecordID:  uuid.UUID(historyRecord.Bytes),
			ArchiveID: uuid.UUID(historyArchive.Bytes),
		}
	}
	if !source.Valid && !historyRecord.Valid {
		deadline := d.CreatedAt.Add(policy.Retention).UTC()
		d.SourceExpiresAt = &deadline
		d.SourceExpired = expired.Valid || !now.Before(deadline)
		if content && d.SourceExpired {
			return d, nil, ErrAvatarSourceExpired
		}
	}
	d.SourceSHA256, d.FrameSHA256 = hex.EncodeToString(digest), hex.EncodeToString(frame)
	d.CreatedAt, d.UpdatedAt = d.CreatedAt.UTC(), d.UpdatedAt.UTC()
	err = json.Unmarshal(transform, &d.Transform)
	return d, original, err
}

func (s *Store) GetAvatarDraft(ctx context.Context, scope ProfileScope, id uuid.UUID) (AvatarDraft, error) {
	d, _, err := s.readOwnAvatarDraft(ctx, scope, id, false)
	return d, err
}

func (s *Store) AvatarDraftOriginal(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
) (AvatarDraft, []byte, error) {
	return s.readOwnAvatarDraft(ctx, scope, id, true)
}

func (s *Store) readOwnAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
	content bool,
) (AvatarDraft, []byte, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarDraft{}, nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarDraft{}, nil, err
	}
	return readAvatarDraft(ctx, tx, scope.UserID, id, content)
}
func (s *Store) ListAvatarDrafts(ctx context.Context, scope ProfileScope) ([]AvatarDraft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	policy, now, err := avatarSourcePolicy(ctx, tx)
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(
		ctx,
		`SELECT id FROM shuttle_avatar_drafts WHERE user_id=$1 AND (source_file_id IS NOT NULL OR history_record_id IS NOT NULL OR (original_expired_at IS NULL AND created_at > $3)) ORDER BY updated_at DESC,id LIMIT $2`,
		scope.UserID,
		MaxAvatarDraftsPerOwner,
		now.Add(-policy.Retention),
	)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	result := make([]AvatarDraft, 0, len(ids))
	for _, id := range ids {
		d, _, readErr := readAvatarDraft(ctx, tx, scope.UserID, id, false)
		if readErr != nil {
			return nil, readErr
		}
		result = append(result, d)
	}
	return result, nil
}

func (s *Store) AvatarDraftOperation(
	ctx context.Context,
	scope ProfileScope,
	operation uuid.UUID,
) (AvatarDraft, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarDraft{}, err
	}
	result, err := avatarDraftOperation(ctx, tx, scope.UserID, operation, nil)
	if err != nil {
		return AvatarDraft{}, err
	}
	return *result, nil
}

// AvatarDraftPreflight resolves successful imports before Telegram reads. Upload
// callers must hash their actual bounded body before calling this method.
func (s *Store) AvatarDraftPreflight(
	ctx context.Context,
	scope ProfileScope,
	a AvatarDraftCreate,
) (*AvatarDraft, error) {
	hash, _, err := avatarDraftCreateHash(a)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	result, err := avatarDraftOperation(ctx, tx, scope.UserID, a.OperationID, hash)
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if a.SourceFileID != uuid.Nil {
		if err = ownedSourceFile(ctx, tx, scope.UserID, a.SourceFileID); err != nil {
			return nil, err
		}
	}
	if a.HistorySource != nil {
		if _, err = ownedAvatarHistory(ctx, tx, s.q.WithTx(tx), scope, *a.HistorySource); err != nil {
			return nil, err
		}
	}
	return nil, avatarDraftQuota(ctx, tx, scope.UserID, 0)
}
