package db

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

const (
	historyPageItemBytes = 32 << 10
	historyTextRunes     = 4096
)

type HistoryItem struct {
	ID                uuid.UUID       `json:"id"`
	SourceInstance    string          `json:"source_instance"`
	Collection        string          `json:"collection"`
	SourceKey         string          `json:"source_key"`
	SourceRole        string          `json:"source_role"`
	ArchiveID         uuid.UUID       `json:"archive_id"`
	SourceLine        int32           `json:"source_line"`
	ProjectionVersion string          `json:"projection_version"`
	Preview           json.RawMessage `json:"preview"`
}

// HistoryPage is an incomplete source transcript, never fresh agent
// instructions or evidence that a payment has been confirmed in the new system.
type HistoryPage struct {
	Items                   []HistoryItem `json:"items"`
	HasMore                 bool          `json:"has_more"`
	NextID                  uuid.UUID     `json:"next_id,omitempty"`
	CompleteTelegramHistory bool          `json:"complete_telegram_history"`
}

type HistoryText struct {
	ID         uuid.UUID `json:"id"`
	Content    string    `json:"content"`
	StartRune  int32     `json:"start_rune"`
	NextRune   int32     `json:"next_rune"`
	TotalRunes int32     `json:"total_runes"`
	HasMore    bool      `json:"has_more"`
}

// historyOwner locks the current account and reads its current Telegram binding
// in a fresh statement. Imported identities and caller-supplied owner IDs are
// never used to authorize a historical read.
func historyOwner(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope ProfileScope) (int64, error) {
	if err := lockProfileOwner(ctx, tx, scope, false); err != nil {
		return 0, err
	}
	user, err := q.GetUserBySubject(
		ctx,
		sqlc.GetUserBySubjectParams{BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject},
	)
	if err != nil {
		return 0, err
	}
	if user.ID != scope.UserID || user.Revoked {
		return 0, pgx.ErrNoRows
	}
	return user.TgUserID, nil
}

// OwnerHistory returns bounded previews in stable ID order, not date order.
// Full message text is available through OwnerHistoryText in Unicode-rune ranges.
func (s *Store) OwnerHistory(
	ctx context.Context,
	scope ProfileScope,
	after uuid.UUID,
	limit int32,
) (HistoryPage, error) {
	result := HistoryPage{Items: []HistoryItem{}}
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 20 {
		return result, ErrInvalidHistory
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return result, err
	}
	rows, err := q.OwnerHistoryPage(
		ctx,
		sqlc.OwnerHistoryPageParams{
			BotID:           scope.BotID,
			OrganizationID:  scope.Organization,
			OwnerTelegramID: owner,
			ID:              after,
			Limit:           limit + 1,
		},
	)
	if err != nil {
		return result, err
	}
	encodedBytes := 0
	for _, row := range rows {
		item := HistoryItem{
			ID:                row.ID,
			SourceInstance:    row.SourceInstance,
			Collection:        row.CollectionName,
			SourceKey:         row.SourceKey,
			SourceRole:        row.SourceRole,
			ArchiveID:         row.FirstArchiveID,
			SourceLine:        row.FirstLine,
			ProjectionVersion: row.ProjectionVersion,
			Preview:           row.Preview,
		}
		raw, marshalErr := json.Marshal(item)
		if marshalErr != nil {
			return result, marshalErr
		}
		if len(result.Items) == int(limit) || encodedBytes+len(raw) > historyPageItemBytes {
			// A single preview is bounded by the projection whitelist and SQL view.
			if len(result.Items) == 0 {
				return result, ErrInvalidHistory
			}
			result.HasMore = true
			result.NextID = result.Items[len(result.Items)-1].ID
			break
		}
		result.Items = append(result.Items, item)
		encodedBytes += len(raw)
	}
	return result, nil
}

// OwnerHistoryText exposes only the selected owner's historical message content.
// Range offsets count Unicode code points, including when a range splits a glyph.
func (s *Store) OwnerHistoryText(
	ctx context.Context,
	scope ProfileScope,
	id uuid.UUID,
	start int32,
) (HistoryText, error) {
	result := HistoryText{ID: id, StartRune: start}
	if id == uuid.Nil || start < 0 || start > 8<<20 {
		return result, ErrInvalidHistory
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	owner, err := historyOwner(ctx, tx, q, scope)
	if err != nil {
		return result, err
	}
	row, err := q.OwnerHistoryText(
		ctx,
		sqlc.OwnerHistoryTextParams{
			ID:              id,
			BotID:           scope.BotID,
			OrganizationID:  scope.Organization,
			OwnerTelegramID: owner,
			StartRune:       start,
		},
	)
	if err != nil {
		return result, err
	}
	if start > row.TotalRunes {
		return result, ErrInvalidHistory
	}
	result.Content, result.TotalRunes = row.Content, row.TotalRunes
	result.NextRune = start + min(historyTextRunes, row.TotalRunes-start)
	result.HasMore = result.NextRune < result.TotalRunes
	return result, nil
}
