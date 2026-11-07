package db

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

// MaxAvatarDraftsPerOwner bounds admission; it is not a retention or deletion policy.
const MaxAvatarDraftsPerOwner = 5
const MaxAvatarOriginalBytesPerOwner = 50 << 20

var ErrAvatarSourceExpired = errors.New("avatar source expired; upload the original again")

var ErrAvatarDraftQuota = errors.New("avatar original storage limit reached")

// AvatarHistorySource identifies one applied historical file record.
type AvatarHistorySource struct {
	RecordID  uuid.UUID `json:"record_id"`
	ArchiveID uuid.UUID `json:"archive_id"`
}

// AvatarDraftRender carries validated transient input through atomic finalization.
// Telegram originals are never retained by this value's storage path.
type AvatarDraftRender struct {
	Original []byte
	Output   avatar.Rendered
}

// AvatarDraft metadata contains no original bytes or bearer credentials.
type AvatarDraft struct {
	SourceExpiresAt *time.Time           `json:"source_expires_at,omitempty"`
	SourceExpired   bool                 `json:"source_expired,omitempty"`
	HistorySource   *AvatarHistorySource `json:"history_source,omitempty"`
	ID              uuid.UUID            `json:"id"`
	SourceFileID    *uuid.UUID           `json:"source_file_id,omitempty"`
	SourceSHA256    string               `json:"source_sha256"`
	MIMEType        string               `json:"mime_type"`
	SizeBytes       int64                `json:"size_bytes"`
	FrameSHA256     string               `json:"frame_sha256"`
	Transform       avatar.Transform     `json:"transform"`
	Version         int64                `json:"version"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

type AvatarDraftCreate struct {
	HistorySource *AvatarHistorySource
	OperationID   uuid.UUID
	SourceFileID  uuid.UUID
	// UploadSHA256 is computed from the received body by the server; never client metadata.
	UploadSHA256 string
}
type AvatarDraftSave struct {
	OperationID     uuid.UUID        `json:"operation_id"`
	DraftID         uuid.UUID        `json:"draft_id"`
	ExpectedVersion int64            `json:"expected_version"`
	Transform       avatar.Transform `json:"transform"`
}

type avatarDraftCommand struct {
	Kind            string            `json:"kind"`
	SourceFileID    uuid.UUID         `json:"source_file_id,omitempty"`
	UploadSHA256    string            `json:"upload_sha256,omitempty"`
	DraftID         uuid.UUID         `json:"draft_id,omitempty"`
	ExpectedVersion int64             `json:"expected_version,omitempty"`
	Transform       *avatar.Transform `json:"transform,omitempty"`
}

func avatarDraftCreateHash(a AvatarDraftCreate) ([]byte, string, error) {
	if a.HistorySource != nil {
		if a.OperationID == uuid.Nil || a.SourceFileID != uuid.Nil || a.UploadSHA256 != "" ||
			a.HistorySource.RecordID == uuid.Nil || a.HistorySource.ArchiveID == uuid.Nil {
			return nil, "", ErrInvalidAvatar
		}
		command := struct {
			Kind   string              `json:"kind"`
			Source AvatarHistorySource `json:"history_source"`
		}{
			Kind: "create_history", Source: *a.HistorySource,
		}
		raw, err := json.Marshal(command)
		if err != nil {
			return nil, "", err
		}
		hash := sha256.Sum256(raw)
		return hash[:], command.Kind, nil
	}
	if a.OperationID == uuid.Nil || (a.SourceFileID == uuid.Nil) == (a.UploadSHA256 == "") {
		return nil, "", ErrInvalidAvatar
	}
	kind := "create_file"
	if a.SourceFileID == uuid.Nil {
		decoded, err := hex.DecodeString(a.UploadSHA256)
		if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != a.UploadSHA256 {
			return nil, "", ErrInvalidAvatar
		}
		kind = "create_upload"
	}
	hash, err := avatarDraftCommandHash(
		avatarDraftCommand{Kind: kind, SourceFileID: a.SourceFileID, UploadSHA256: a.UploadSHA256},
	)
	return hash, kind, err
}
func avatarDraftSaveHash(a AvatarDraftSave) ([]byte, avatar.Transform, error) {
	if a.OperationID == uuid.Nil || a.DraftID == uuid.Nil || a.ExpectedVersion < 1 {
		return nil, avatar.Transform{}, ErrInvalidAvatar
	}
	transform, err := avatar.NormalizeTransform(&a.Transform)
	if err != nil {
		return nil, transform, err
	}
	hash, err := avatarDraftCommandHash(
		avatarDraftCommand{
			Kind:            "save_transform",
			DraftID:         a.DraftID,
			ExpectedVersion: a.ExpectedVersion,
			Transform:       &transform,
		},
	)
	return hash, transform, err
}
func avatarDraftCommandHash(command avatarDraftCommand) ([]byte, error) {
	raw, err := json.Marshal(command)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	return hash[:], nil
}
