package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/files"
)

func ownedSourceFile(ctx context.Context, tx pgx.Tx, ownerID, fileID uuid.UUID) error {
	var id uuid.UUID
	return tx.QueryRow(ctx, `SELECT id FROM shuttle_files WHERE id=$1 AND user_id=$2`, fileID, ownerID).Scan(&id)
}

func insertAttachments(ctx context.Context, q *sqlc.Queries, userID, taskID uuid.UUID, items []files.Incoming) error {
	for _, item := range items {
		if !item.Valid() {
			return files.ErrInvalid
		}
		if err := q.CreateFile(
			ctx,
			sqlc.CreateFileParams{
				ID:         item.ID,
				UserID:     userID,
				TaskID:     taskID,
				TelegramID: item.TelegramID,
				UniqueID:   item.UniqueID,
				Kind:       item.Kind,
				MimeType:   item.MIMEType,
				Filename:   item.Filename,
				SizeBytes:  item.SizeBytes,
				MessageID:  item.MessageID,
				SentAt:     pgtype.Timestamptz{Time: item.SentAt, Valid: true},
			},
		); err != nil {
			return err
		}
	}
	return nil
}

type File struct {
	files.Metadata

	TelegramID string
}

// File checks the current active identity and exact deployment before exposing any provenance.
func (s *Store) File(ctx context.Context, scope EventContextScope, id uuid.UUID) (File, error) {
	row, err := s.q.GetOwnedFile(
		ctx,
		sqlc.GetOwnedFileParams{
			ID:             id,
			ID_2:           scope.UserID,
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return File{}, err
	}
	return storedFile(row), nil
}

func storedFile(row sqlc.ShuttleFile) File {
	metadata := files.Metadata{
		ID:         row.ID,
		TaskID:     row.TaskID,
		Kind:       row.Kind,
		MIMEType:   row.MimeType,
		Filename:   row.Filename,
		SizeBytes:  row.SizeBytes,
		MessageID:  row.MessageID,
		SentAt:     row.SentAt.Time.UTC(),
		AcceptedAt: row.AcceptedAt.Time.UTC(),
	}
	return File{Metadata: metadata,
		TelegramID: row.TelegramID,
	}
}
