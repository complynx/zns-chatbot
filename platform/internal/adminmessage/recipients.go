package adminmessage

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// ResolveShortcut snapshots $event[:category]. Enqueue uses the reviewed snapshot,
// never silently re-expands a growing audience after confirmation.
func (s Service) ResolveShortcut(ctx context.Context, actor, shortcut string) ([]Destination, error) {
	if !strings.HasPrefix(shortcut, "$") {
		return nil, invalid()
	}
	event, category, found := strings.Cut(strings.TrimPrefix(shortcut, "$"), ":")
	if !found {
		category = audienceAssigned
	}
	category = strings.ToLower(strings.TrimSpace(category))
	if event == "" {
		return nil, invalid()
	}
	switch category {
	case audienceAdmins, "paid", audienceAssigned, "unpaid", "waitlist", "all":
	default:
		return nil, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authorize(ctx, tx, actor); err != nil {
		return nil, err
	}
	result, err := resolveShortcut(ctx, tx, event, category)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit(ctx)
}

func resolveShortcut(ctx context.Context, tx pgx.Tx, event, category string) ([]Destination, error) {
	var err error
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_events WHERE id=$1)`, event).
		Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, problem(http.StatusNotFound, "not_found")
	}
	query := `SELECT u.telegram_id FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner
 WHERE b.event_id=$1 AND b.state<>'cancelled' AND ($2='all' OR ($2='paid' AND b.state='paid')
 OR ($2='assigned' AND b.state IN ('assigned','paid')) OR ($2='unpaid' AND b.state='assigned')
 OR ($2='waitlist' AND b.state NOT IN ('assigned','paid'))) ORDER BY b.created_at,u.telegram_id`
	args := []any{event, category}
	if category == audienceAdmins {
		query = `SELECT u.telegram_id FROM core.pass_payment_admins a JOIN core.users u ON u.id=a.owner
 WHERE a.event_id=$1 ORDER BY u.telegram_id`
		args = []any{event}
	}
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	result, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Destination, error) {
		var id int64
		scanErr := row.Scan(&id)
		return Destination{Chat: strconv.FormatInt(id, 10)}, scanErr
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
