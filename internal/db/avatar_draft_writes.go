package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

type avatarDraftSession struct{ Bearer, Generation [sha256.Size]byte }

func (s *Store) avatarDraftTransaction(
	ctx context.Context,
	scope ProfileScope,
	session *avatarDraftSession,
) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err = lockProfileOwner(ctx, tx, scope, true); err == nil && session != nil {
		var attempt MiniAppAttempt
		attempt, err = s.lockMiniAppBearer(ctx, tx, scope.BotID, session.Bearer, session.Generation)
		if err == nil &&
			(attempt.User.ID != scope.UserID || attempt.User.Organization != scope.Organization || attempt.User.Subject != scope.Subject) {
			err = ErrMiniAppDenied
		}
	}
	if err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, err
	}
	return tx, nil
}

func writeAvatarDraftOperation(
	ctx context.Context,
	tx pgx.Tx,
	owner, operation uuid.UUID,
	kind string,
	hash []byte,
	d AvatarDraft,
	native bool,
) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_avatar_draft_operations(user_id,operation_id,kind,request_hash,draft_id,result,native) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		owner,
		operation,
		kind,
		hash,
		d.ID,
		raw,
		native,
	)
	return err
}

func (s *Store) CreateAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	a AvatarDraftCreate,
	raw []byte,
	proof avatar.Rendered,
) (AvatarDraft, error) {
	return s.createAvatarDraft(ctx, scope, a, raw, proof, nil, false)
}

// CreateSystemAvatarDraft records a native photo workflow without a Mini App session.
func (s *Store) CreateSystemAvatarDraft(
	ctx context.Context, scope ProfileScope, a AvatarDraftCreate, raw []byte, proof avatar.Rendered,
) (AvatarDraft, error) {
	return s.createAvatarDraft(ctx, scope, a, raw, proof, nil, true)
}

func (s *Store) CreateMiniAppAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	bearer, generation [sha256.Size]byte,
	a AvatarDraftCreate,
	raw []byte,
	proof avatar.Rendered,
) (AvatarDraft, error) {
	return s.createAvatarDraft(ctx, scope, a, raw, proof, &avatarDraftSession{bearer, generation}, true)
}

func (s *Store) createAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	a AvatarDraftCreate,
	raw []byte,
	proof avatar.Rendered,
	session *avatarDraftSession,
	native bool,
) (AvatarDraft, error) {
	hash, kind, err := avatarDraftCreateHash(a)
	if err != nil {
		return AvatarDraft{}, err
	}
	tx, err := s.avatarDraftTransaction(ctx, scope, session)
	if err != nil {
		return AvatarDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := avatarDraftOperation(ctx, tx, scope.UserID, a.OperationID, hash)
	if err == nil {
		return *prior, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AvatarDraft{}, err
	}
	digest, mime, err := validateAvatarDraftOriginal(a, raw, proof)
	if err != nil {
		return AvatarDraft{}, err
	}
	if err = validateAvatarDraftSource(
		ctx,
		tx,
		s.q.WithTx(tx),
		scope,
		a.SourceFileID,
		a.HistorySource,
		mime,
	); err != nil {
		return AvatarDraft{}, err
	}
	stored := raw
	if a.SourceFileID != uuid.Nil || a.HistorySource != nil {
		stored = nil
	}
	if err = avatarDraftQuota(ctx, tx, scope.UserID, len(stored)); err != nil {
		return AvatarDraft{}, err
	}
	transform, err := json.Marshal(avatar.IdentityTransform())
	if err != nil {
		return AvatarDraft{}, err
	}
	frame, err := hex.DecodeString(avatar.FrameSHA256)
	if err != nil {
		return AvatarDraft{}, err
	}
	id := uuid.New()
	var source any
	if a.SourceFileID != uuid.Nil {
		source = a.SourceFileID
	}
	var historyRecord, historyArchive any
	if a.HistorySource != nil {
		historyRecord = a.HistorySource.RecordID
		historyArchive = a.HistorySource.ArchiveID
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_avatar_drafts(id,user_id,source_file_id,original,source_sha256,mime_type,frame_sha256,transform,version,source_size_bytes,history_record_id,history_archive_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$10,$11)`,
		id,
		scope.UserID,
		source,
		stored,
		digest[:],
		mime,
		frame,
		transform,
		int64(len(raw)),
		historyRecord,
		historyArchive,
	)
	if err != nil {
		return AvatarDraft{}, err
	}
	d, _, err := readAvatarDraft(ctx, tx, scope.UserID, id, false)
	if err != nil {
		return d, err
	}
	if err = writeAvatarDraftOperation(
		ctx,
		tx,
		scope.UserID,
		a.OperationID,
		kind,
		hash,
		d,
		native,
	); err != nil {
		return AvatarDraft{}, err
	}
	return d, tx.Commit(ctx)
}

func (s *Store) SaveAvatarDraft(ctx context.Context, scope ProfileScope, a AvatarDraftSave) (AvatarDraft, error) {
	return s.saveAvatarDraft(ctx, scope, a, nil)
}

func (s *Store) SaveMiniAppAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	bearer, generation [sha256.Size]byte,
	a AvatarDraftSave,
) (AvatarDraft, error) {
	return s.saveAvatarDraft(ctx, scope, a, &avatarDraftSession{bearer, generation})
}

func (s *Store) saveAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	a AvatarDraftSave,
	session *avatarDraftSession,
) (AvatarDraft, error) {
	hash, transform, err := avatarDraftSaveHash(a)
	if err != nil {
		return AvatarDraft{}, err
	}
	tx, err := s.avatarDraftTransaction(ctx, scope, session)
	if err != nil {
		return AvatarDraft{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := avatarDraftOperation(ctx, tx, scope.UserID, a.OperationID, hash)
	if err == nil {
		return *prior, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AvatarDraft{}, err
	}
	d, _, err := readAvatarDraft(ctx, tx, scope.UserID, a.DraftID, false)
	if err != nil {
		return d, err
	}
	if d.SourceExpired {
		return AvatarDraft{}, ErrAvatarSourceExpired
	}
	if d.Version != a.ExpectedVersion {
		return AvatarDraft{}, ErrConflict
	}
	encoded, err := json.Marshal(transform)
	if err != nil {
		return AvatarDraft{}, err
	}
	tag, err := tx.Exec(
		ctx,
		`UPDATE shuttle_avatar_drafts SET transform=$3,version=version+1,updated_at=clock_timestamp() WHERE user_id=$1 AND id=$2 AND version=$4`,
		scope.UserID,
		a.DraftID,
		encoded,
		a.ExpectedVersion,
	)
	if err != nil {
		return AvatarDraft{}, err
	}
	if tag.RowsAffected() != 1 {
		return AvatarDraft{}, ErrConflict
	}
	d, _, err = readAvatarDraft(ctx, tx, scope.UserID, a.DraftID, false)
	if err != nil {
		return d, err
	}
	if err = writeAvatarDraftOperation(
		ctx,
		tx,
		scope.UserID,
		a.OperationID,
		"save_transform",
		hash,
		d,
		session != nil,
	); err != nil {
		return AvatarDraft{}, err
	}
	return d, tx.Commit(ctx)
}

func validateAvatarDraftOriginal(
	a AvatarDraftCreate,
	raw []byte,
	proof avatar.Rendered,
) ([sha256.Size]byte, string, error) {
	var digest [sha256.Size]byte
	if len(raw) == 0 || len(raw) > avatar.MaxInputBytes {
		return digest, "", avatar.ErrImageLimit
	}
	digest = sha256.Sum256(raw)
	sourceHash := hex.EncodeToString(digest[:])
	if proof.SourceSHA256 != sourceHash || validRenderedAvatar(proof, nil) != nil ||
		(a.SourceFileID == uuid.Nil && a.HistorySource == nil && a.UploadSHA256 != sourceHash) {
		return digest, "", ErrInvalidAvatar
	}
	mime := http.DetectContentType(raw)
	if mime != "image/jpeg" && mime != "image/png" {
		return digest, "", avatar.ErrInvalidImage
	}
	return digest, mime, nil
}
