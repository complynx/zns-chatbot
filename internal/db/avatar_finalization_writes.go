package db

import (
	"bytes"
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

func (s *Store) CommitAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	a AvatarDraftFinalize,
	rendered AvatarDraftRender,
) (AvatarDraftReceipt, error) {
	return s.commitAvatarDraft(ctx, scope, a, rendered, nil, false)
}

// CommitSystemAvatarDraft admits a native photo document without creating a Loom run.
func (s *Store) CommitSystemAvatarDraft(
	ctx context.Context, scope ProfileScope, a AvatarDraftFinalize, rendered AvatarDraftRender,
) (AvatarDraftReceipt, error) {
	return s.commitAvatarDraft(ctx, scope, a, rendered, nil, true)
}

func (s *Store) CommitMiniAppAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	bearer, generation [sha256.Size]byte,
	a AvatarDraftFinalize,
	rendered AvatarDraftRender,
) (AvatarDraftReceipt, error) {
	return s.commitAvatarDraft(ctx, scope, a, rendered, &avatarDraftSession{bearer, generation}, true)
}

// commitAvatarDraft serializes owner admission with draft edits and the shared
// legacy artifact quota. Bytes, immutable receipt and delivery intent commit
// together; no Loom task or task outbox entry is created.
func (s *Store) commitAvatarDraft(
	ctx context.Context,
	scope ProfileScope,
	a AvatarDraftFinalize,
	rendered AvatarDraftRender,
	session *avatarDraftSession,
	native bool,
) (AvatarDraftReceipt, error) {
	hash, err := avatarFinalizeHash(a)
	if err != nil {
		return AvatarDraftReceipt{}, err
	}
	tx, err := s.avatarDraftTransaction(ctx, scope, session)
	if err != nil {
		return AvatarDraftReceipt{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	prior, err := avatarFinalizationReceipt(ctx, tx, scope.UserID, a.OperationID, hash)
	if err == nil {
		return *prior, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return AvatarDraftReceipt{}, err
	}
	d, storedOriginal, err := readAvatarDraft(ctx, tx, scope.UserID, a.DraftID, true)
	if err != nil {
		return AvatarDraftReceipt{}, err
	}
	if d.Version != a.ExpectedVersion || d.FrameSHA256 != avatar.FrameSHA256 {
		return AvatarDraftReceipt{}, ErrConflict
	}
	if d.SourceFileID == nil && d.HistorySource == nil && !bytes.Equal(storedOriginal, rendered.Original) {
		return AvatarDraftReceipt{}, ErrInvalidAvatar
	}
	if err = validateFinalizedAvatar(d, rendered.Original, rendered.Output); err != nil {
		return AvatarDraftReceipt{}, err
	}
	var sourceID uuid.UUID
	if d.SourceFileID != nil {
		sourceID = *d.SourceFileID
	}
	if err = validateAvatarDraftSource(
		ctx,
		tx,
		s.q.WithTx(tx),
		scope,
		sourceID,
		d.HistorySource,
		d.MIMEType,
	); err != nil {
		return AvatarDraftReceipt{}, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM shuttle_avatars WHERE user_id=$1`, scope.UserID).
		Scan(&count); err != nil {
		return AvatarDraftReceipt{}, err
	}
	if count >= MaxAvatarsPerOwner {
		return AvatarDraftReceipt{}, ErrAvatarQuota
	}
	return writeFinalizedAvatar(ctx, tx, scope, a, hash, d, rendered.Output, native)
}
func validateFinalizedAvatar(d AvatarDraft, raw []byte, r avatar.Rendered) error {
	digest := sha256.Sum256(raw)
	if int64(len(raw)) != d.SizeBytes || http.DetectContentType(raw) != d.MIMEType ||
		r.SourceSHA256 != d.SourceSHA256 ||
		d.SourceSHA256 != hex.EncodeToString(digest[:]) ||
		r.FrameSHA256 != d.FrameSHA256 ||
		!bytes.HasPrefix(r.JPEG, []byte{0xff, 0xd8}) {
		return ErrInvalidAvatar
	}
	return validRenderedAvatar(r, &d.Transform)
}

func writeFinalizedAvatar(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	a AvatarDraftFinalize,
	hash []byte,
	d AvatarDraft,
	r avatar.Rendered,
	native bool,
) (AvatarDraftReceipt, error) {
	var recipient int64
	result := AvatarDraftReceipt{
		ArtifactID:       uuid.New(),
		DeliveryID:       uuid.New(),
		DraftID:          d.ID,
		DraftVersion:     d.Version,
		SourceSHA256:     r.SourceSHA256,
		FrameSHA256:      r.FrameSHA256,
		ProcessorVersion: r.ProcessorVersion,
		Transform:        d.Transform,
		SHA256:           r.SHA256,
		Width:            r.Width,
		Height:           r.Height,
		SizeBytes:        int64(len(r.JPEG)),
	}
	if err := tx.QueryRow(ctx, `SELECT tg_user_id,clock_timestamp() FROM shuttle_users WHERE id=$1`, scope.UserID).
		Scan(&recipient, &result.CreatedAt); err != nil {
		return result, err
	}
	result.CreatedAt = result.CreatedAt.UTC()
	receipt, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_avatars(id,user_id,operation_id,request_hash,receipt,jpeg,"+
			"draft_id,draft_version,native,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)",
		result.ArtifactID,
		scope.UserID,
		a.OperationID,
		hash,
		receipt,
		r.JPEG,
		d.ID,
		d.Version,
		native,
		result.CreatedAt,
	)
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_avatar_deliveries(id,artifact_id,user_id,telegram_user_id,created_at) VALUES($1,$2,$3,$4,$5)`,
		result.DeliveryID,
		result.ArtifactID,
		scope.UserID,
		recipient,
		result.CreatedAt,
	)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
