package passbooking

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
)

const MaxExportEvents = 1000

// MaxExportAuthorityBytes allows the complete bounded event set even when every
// identifier byte uses a six-byte JSON escape.
const MaxExportAuthorityBytes = 2 << 20

// ExportSnapshot binds the file to every event read in its original transaction.
// The trusted delivery adapter must recheck Events before sending Body.
type ExportSnapshot struct {
	Body   []byte   `json:"body"`
	Events []string `json:"events"`
}

func ValidExportEvents(events []string) bool {
	if len(events) == 0 || len(events) > MaxExportEvents || !slices.IsSorted(events) {
		return false
	}
	for i, event := range events {
		if event == "" || len(event) > 200 || strings.ContainsRune(event, 0) || (i > 0 && event == events[i-1]) {
			return false
		}
	}
	return true
}

func (s Service) Export(ctx context.Context, actor string) ([]byte, error) {
	snapshot, err := s.ExportSnapshot(ctx, actor)
	return snapshot.Body, err
}

func exportSnapshotEvents(ctx context.Context, tx pgx.Tx, actor string) ([]string, error) {
	rows, err := tx.Query(
		ctx,
		exportEvents+`SELECT id FROM allowed_events ORDER BY id LIMIT $2`,
		actor,
		MaxExportEvents+1,
	)
	if err != nil {
		return nil, err
	}
	events, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	if !ValidExportEvents(events) {
		return nil, exportTooLarge()
	}
	return events, nil
}

// CheckExportSnapshot refuses a partially revoked export even if another event
// still grants export capability. It performs no external delivery.
func (s Service) CheckExportSnapshot(ctx context.Context, actor string, events []string) error {
	if !ValidExportEvents(events) {
		return invalid()
	}
	var allowed bool
	err := s.DB.QueryRow(ctx, exportEvents+`SELECT count(*)=$3 FROM allowed_events WHERE id=ANY($2::text[])`, actor, events, len(events)).
		Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return forbidden()
	}
	return nil
}
