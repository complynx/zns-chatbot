package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// AvatarDraftInput is a server-only source snapshot. Original contains bytes only
// for browser uploads; TelegramID is resolved under the current account lock.
type AvatarDraftInput struct {
	Draft      AvatarDraft
	Original   []byte
	TelegramID string
}

func ownedAvatarHistory(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope ProfileScope,
	source AvatarHistorySource,
) (sqlc.OwnerHistoryAvatarSourceRow, error) {
	owner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return sqlc.OwnerHistoryAvatarSourceRow{}, err
	}
	return q.OwnerHistoryAvatarSource(ctx, sqlc.OwnerHistoryAvatarSourceParams{
		ID:              source.RecordID,
		ID_2:            source.ArchiveID,
		BotID:           scope.BotID,
		OrganizationID:  scope.Organization,
		OwnerTelegramID: owner,
	})
}

// validateAvatarDraftSource performs no network IO or nested pool acquisition.
func validateAvatarDraftSource(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope ProfileScope,
	fileID uuid.UUID, history *AvatarHistorySource, mime string,
) error {
	if fileID != uuid.Nil {
		return ownedSourceFile(ctx, tx, scope.UserID, fileID)
	}
	if history == nil {
		return nil
	}
	row, err := ownedAvatarHistory(ctx, tx, q, scope, *history)
	if err != nil {
		return err
	}
	source, err := readHistoryAvatarSource(ctx, q, row)
	if err != nil {
		return err
	}
	if source.MIMEType != mime {
		return ErrInvalidAvatar
	}
	return nil
}

// AvatarDraftSource resolves one owned input without acquiring another pool
// connection. The caller rechecks request authority after remote processing.
func (s *Store) AvatarDraftSource(ctx context.Context, scope ProfileScope, id uuid.UUID) (AvatarDraftInput, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return AvatarDraftInput{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return AvatarDraftInput{}, err
	}
	d, raw, err := readAvatarDraft(ctx, tx, scope.UserID, id, true)
	if err != nil {
		return AvatarDraftInput{}, err
	}
	result := AvatarDraftInput{Draft: d, Original: raw}
	if d.SourceFileID != nil {
		err = tx.QueryRow(ctx, `SELECT telegram_id FROM shuttle_files WHERE id=$1 AND user_id=$2`, *d.SourceFileID, scope.UserID).
			Scan(&result.TelegramID)
	} else if d.HistorySource != nil {
		var row sqlc.OwnerHistoryAvatarSourceRow
		row, err = ownedAvatarHistory(ctx, tx, s.q.WithTx(tx), scope, *d.HistorySource)
		if err == nil {
			source, readErr := readHistoryAvatarSource(ctx, s.q.WithTx(tx), row)
			err = readErr
			result.TelegramID = source.FileID
		}
	}
	if err != nil {
		return AvatarDraftInput{}, err
	}
	return result, nil
}
