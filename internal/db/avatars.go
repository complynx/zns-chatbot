package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

const MaxAvatarsPerOwner = 20
const avatarRunBindingPending = "run_binding_pending"

var (
	ErrInvalidAvatar    = errors.New("invalid avatar request")
	ErrAvatarQuota      = errors.New("avatar storage limit reached; at most 20 generated avatars per owner")
	ErrAvatarRunPending = errors.New("avatar run binding is not yet persisted")
)

type AvatarRequest struct {
	OperationID  uuid.UUID         `json:"operation_id"`
	RunID        string            `json:"run_id"`
	SourceFileID uuid.UUID         `json:"source_file_id"`
	Transform    *avatar.Transform `json:"transform,omitempty"`
}

// AvatarReceipt is the original generation result. Delivery progress is read separately.
type AvatarReceipt struct {
	ArtifactID       uuid.UUID         `json:"artifact_id"`
	TaskID           uuid.UUID         `json:"task_id"`
	DeliveryID       uuid.UUID         `json:"delivery_id"`
	SourceFileID     uuid.UUID         `json:"source_file_id"`
	SourceSHA256     string            `json:"source_sha256"`
	FrameSHA256      string            `json:"frame_sha256"`
	ProcessorVersion string            `json:"processor_version"`
	SHA256           string            `json:"sha256"`
	Width            int               `json:"width"`
	Height           int               `json:"height"`
	SizeBytes        int64             `json:"size_bytes"`
	CreatedAt        time.Time         `json:"created_at"`
	Transform        *avatar.Transform `json:"transform,omitempty"`
}

type AvatarView struct {
	Avatar        AvatarReceipt `json:"avatar"`
	DeliveryState string        `json:"delivery_state"`
}

func avatarRequestHash(request AvatarRequest) ([]byte, error) {
	if request.OperationID == uuid.Nil || request.SourceFileID == uuid.Nil || request.RunID == "" ||
		len(request.RunID) > 128 {
		return nil, ErrInvalidAvatar
	}
	transform, err := avatar.NormalizeTransform(request.Transform)
	if err != nil {
		return nil, ErrInvalidAvatar
	}
	request.Transform = nil
	if transform != avatar.IdentityTransform() {
		request.Transform = &transform
	}
	raw, err := json.Marshal(request)
	hash := sha256.Sum256(raw)
	return hash[:], err
}

func avatarReceipt(
	ctx context.Context,
	tx pgx.Tx,
	ownerID, operationID uuid.UUID,
	hash []byte,
) (*AvatarReceipt, error) {
	var storedHash, raw []byte
	var draft pgtype.UUID
	err := tx.QueryRow(ctx, "SELECT request_hash,receipt,draft_id FROM shuttle_avatars WHERE user_id=$1 AND "+
		"operation_id=$2", ownerID, operationID).
		Scan(&storedHash, &raw, &draft)
	if err != nil {
		return nil, err
	}
	if hash != nil && (draft.Valid || !bytes.Equal(hash, storedHash)) {
		return nil, ErrConflict
	}
	if draft.Valid {
		return nil, pgx.ErrNoRows
	}
	var result AvatarReceipt
	err = json.Unmarshal(raw, &result)
	return &result, err
}

func avatarTarget(
	ctx context.Context,
	tx pgx.Tx,
	ownerID uuid.UUID,
	request AvatarRequest,
	lock bool,
) (uuid.UUID, string, error) {
	query := "SELECT t.id,CASE WHEN t.run_id='' THEN 'run_binding_pending'\n" +
		"WHEN EXISTS(SELECT 1 FROM shuttle_conversations c WHERE c.id=t.conversation_id AND " +
		"c.active_task_id=t.id AND c.active_run_id=t.run_id) THEN t.state ELSE 'closed' END\n" +
		"FROM shuttle_tasks t WHERE t.user_id=$1 AND\n" +
		"(t.run_id=$2 OR (t.run_id='' AND t.state IN ('creating_run','awaiting_service') AND " +
		"t.snapshot->'create' IS NOT NULL\n" +
		" AND EXISTS(SELECT 1 FROM shuttle_conversations c WHERE c.id=t.conversation_id AND " +
		"c.active_task_id=t.id)))\n" +
		"ORDER BY (t.run_id=$2) DESC,t.created_at,t.id LIMIT 1"
	if lock {
		query += ` FOR UPDATE OF t`
	}
	var id uuid.UUID
	var state string
	err := tx.QueryRow(ctx, query, ownerID, request.RunID).Scan(&id, &state)
	return id, state, err
}

// AvatarPreflight returns a stable receipt before requiring a live run or Telegram download.
func (s *Store) AvatarPreflight(
	ctx context.Context,
	scope ProfileScope,
	request AvatarRequest,
) (*AvatarReceipt, error) {
	hash, err := avatarRequestHash(request)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	prior, err := avatarReceipt(ctx, tx, scope.UserID, request.OperationID, hash)
	if (err != nil && !errors.Is(err, pgx.ErrNoRows)) || prior != nil {
		return prior, err
	}
	_, state, err := avatarTarget(ctx, tx, scope.UserID, request, false)
	if err != nil {
		return nil, err
	}
	if state == avatarRunBindingPending {
		return nil, ErrAvatarRunPending
	}
	if terminalTask(state) {
		return nil, ErrConflict
	}
	return nil, ownedSourceFile(ctx, tx, scope.UserID, request.SourceFileID)
}

// CommitAvatar locks the current task before the owner, then commits bytes, receipt and delivery together.
func (s *Store) CommitAvatar(
	ctx context.Context,
	scope ProfileScope,
	request AvatarRequest,
	rendered avatar.Rendered,
) (AvatarReceipt, error) {
	hash, err := avatarRequestHash(request)
	if err != nil {
		return AvatarReceipt{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarReceipt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	taskID, _, err := avatarTarget(ctx, tx, scope.UserID, request, true)
	if err != nil {
		return AvatarReceipt{}, err
	}
	if err = lockProfileOwner(ctx, tx, scope, true); err != nil {
		return AvatarReceipt{}, err
	}
	prior, err := avatarReceipt(ctx, tx, scope.UserID, request.OperationID, hash)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return AvatarReceipt{}, err
	}
	if prior != nil {
		return *prior, nil
	}
	// The locking SELECT may have waited on a task or identity update. Use a fresh
	// statement snapshot for the current conversation binding after both locks.
	currentID, state, err := avatarTarget(ctx, tx, scope.UserID, request, false)
	if err != nil {
		return AvatarReceipt{}, err
	}
	if state == avatarRunBindingPending {
		return AvatarReceipt{}, ErrAvatarRunPending
	}
	if currentID != taskID || terminalTask(state) {
		return AvatarReceipt{}, ErrConflict
	}
	if err = validRenderedAvatar(rendered, request.Transform); err != nil {
		return AvatarReceipt{}, err
	}
	if err = ownedSourceFile(ctx, tx, scope.UserID, request.SourceFileID); err != nil {
		return AvatarReceipt{}, err
	}
	return commitRenderedAvatar(ctx, tx, scope, request, taskID, hash, rendered)
}

func validRenderedAvatar(rendered avatar.Rendered, requested *avatar.Transform) error {
	transform, err := avatar.NormalizeTransform(requested)
	if err != nil {
		return ErrInvalidAvatar
	}
	digest := sha256.Sum256(rendered.JPEG)
	sourceHash, err := hex.DecodeString(rendered.SourceSHA256)
	if err != nil || len(sourceHash) != sha256.Size || len(rendered.JPEG) == 0 ||
		len(rendered.JPEG) > avatar.MaxOutputBytes ||
		rendered.SHA256 != hex.EncodeToString(digest[:]) ||
		rendered.FrameSHA256 != avatar.FrameSHA256 ||
		!rendered.MatchesTransform(transform) ||
		rendered.Width != avatar.OutputSize ||
		rendered.Height != avatar.OutputSize {
		return ErrInvalidAvatar
	}
	return nil
}

func commitRenderedAvatar(
	ctx context.Context,
	tx pgx.Tx,
	scope ProfileScope,
	request AvatarRequest,
	taskID uuid.UUID,
	hash []byte,
	rendered avatar.Rendered,
) (AvatarReceipt, error) {
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM shuttle_avatars WHERE user_id=$1`, scope.UserID).
		Scan(&count); err != nil {
		return AvatarReceipt{}, err
	}
	if count >= MaxAvatarsPerOwner {
		return AvatarReceipt{}, ErrAvatarQuota
	}
	var recipient int64
	var created time.Time
	if err := tx.QueryRow(ctx, `SELECT tg_user_id,clock_timestamp() FROM shuttle_users WHERE id=$1`, scope.UserID).
		Scan(&recipient, &created); err != nil {
		return AvatarReceipt{}, err
	}
	result := AvatarReceipt{
		ArtifactID:       uuid.New(),
		TaskID:           taskID,
		DeliveryID:       uuid.New(),
		SourceFileID:     request.SourceFileID,
		SourceSHA256:     rendered.SourceSHA256,
		FrameSHA256:      rendered.FrameSHA256,
		ProcessorVersion: rendered.ProcessorVersion,
		SHA256:           rendered.SHA256,
		Width:            rendered.Width,
		Height:           rendered.Height,
		SizeBytes:        int64(len(rendered.JPEG)),
		CreatedAt:        created.UTC(),
		Transform:        rendered.Transform,
	}
	payload, err := json.Marshal(struct {
		Kind       string    `json:"kind"`
		ArtifactID uuid.UUID `json:"artifact_id"`
		ChatID     int64     `json:"chat_id"`
	}{"avatar_document", result.ArtifactID, recipient})
	if err != nil {
		return AvatarReceipt{}, err
	}
	if err = appendOutbox(
		ctx,
		tx,
		taskID,
		[]Outbound{
			{
				ID:       result.DeliveryID,
				DedupKey: taskID.String() + ":avatar:" + request.OperationID.String(),
				Payload:  payload,
			},
		},
	); err != nil {
		return AvatarReceipt{}, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return AvatarReceipt{}, err
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_avatars(id,user_id,task_id,delivery_id,source_file_id,"+
			"operation_id,request_hash,receipt,jpeg,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,"+
			"$10)",
		result.ArtifactID,
		scope.UserID,
		taskID,
		result.DeliveryID,
		request.SourceFileID,
		request.OperationID,
		hash,
		raw,
		rendered.JPEG,
		result.CreatedAt,
	)
	if err != nil {
		return AvatarReceipt{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return AvatarReceipt{}, err
	}
	return result, nil
}

func (s *Store) AvatarOperation(ctx context.Context, scope ProfileScope, operationID uuid.UUID) (AvatarReceipt, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarReceipt{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarReceipt{}, err
	}
	result, err := avatarReceipt(ctx, tx, scope.UserID, operationID, nil)
	if err != nil {
		return AvatarReceipt{}, err
	}
	return *result, nil
}

func (s *Store) GetAvatar(
	ctx context.Context,
	scope ProfileScope,
	artifactID uuid.UUID,
) (AvatarReceipt, []byte, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarReceipt{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarReceipt{}, nil, err
	}
	return readAvatar(ctx, tx, scope.UserID, artifactID, uuid.Nil)
}

func readAvatar(ctx context.Context, tx pgx.Tx, ownerID, artifactID, taskID uuid.UUID) (AvatarReceipt, []byte, error) {
	var raw, jpeg []byte
	err := tx.QueryRow(ctx, "SELECT receipt,jpeg FROM shuttle_avatars WHERE id=$1 AND user_id=$2 AND draft_id IS "+
		"NULL AND ($3::uuid='00000000-0000-0000-0000-000000000000'::uuid OR task_id=$3)", artifactID, ownerID, taskID).
		Scan(&raw, &jpeg)
	if err != nil {
		return AvatarReceipt{}, nil, err
	}
	var result AvatarReceipt
	err = json.Unmarshal(raw, &result)
	return result, jpeg, err
}

func (s *Store) AvatarStatus(ctx context.Context, scope ProfileScope, artifactID uuid.UUID) (AvatarView, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarView{}, err
	}
	var raw []byte
	var result AvatarView
	err = tx.QueryRow(ctx, "SELECT a.receipt,o.state FROM shuttle_avatars a JOIN shuttle_outbox o ON "+
		"o.id=a.delivery_id WHERE a.id=$1 AND a.user_id=$2", artifactID, scope.UserID).
		Scan(&raw, &result.DeliveryState)
	if err != nil {
		return AvatarView{}, err
	}
	err = json.Unmarshal(raw, &result.Avatar)
	return result, err
}

// GetAvatarForDelivery binds persisted bytes to the actual task owner and current deployment identity.
func (s *Store) GetAvatarForDelivery(ctx context.Context, taskID, artifactID uuid.UUID) (AvatarReceipt, []byte, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarReceipt{}, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var scope ProfileScope
	err = tx.QueryRow(ctx, "SELECT u.id,u.bot_id,u.zitadel_org,u.zitadel_subject FROM shuttle_tasks t JOIN "+
		"shuttle_users u ON u.id=t.user_id WHERE t.id=$1 AND NOT u.revoked", taskID).
		Scan(&scope.UserID, &scope.BotID, &scope.Organization, &scope.Subject)
	if err != nil {
		return AvatarReceipt{}, nil, err
	}
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarReceipt{}, nil, err
	}
	return readAvatar(ctx, tx, scope.UserID, artifactID, taskID)
}

// appendOutbox requires the caller to hold the parent task row lock. Sequence allocation must follow that lock.
func appendOutbox(ctx context.Context, tx pgx.Tx, taskID uuid.UUID, out []Outbound) error {
	for _, item := range out {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_outbox WHERE dedup_key=$1)`, item.DedupKey).
			Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		if item.ID == uuid.Nil {
			item.ID = uuid.New()
		}
		_, err := tx.Exec(
			ctx,
			"INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload) SELECT $1,$2,$3,"+
				"coalesce(max(chunk_seq),-1)+1,$4 FROM shuttle_outbox WHERE task_id=$2 ON "+
				"CONFLICT(dedup_key) DO NOTHING",
			item.ID,
			taskID,
			item.DedupKey,
			item.Payload,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
