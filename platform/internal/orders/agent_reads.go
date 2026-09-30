package orders

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type HistoryItem struct {
	ID      string    `json:"id"`
	OrderID string    `json:"order_id"`
	Action  string    `json:"action"`
	Version int64     `json:"version"`
	At      time.Time `json:"at"`
}

// RecentHistory fixes the same latest thirty snapshots as History before their
// details are read. New commits cannot move this selection between chunk reads.
func (s Service) RecentHistory(ctx context.Context, owner, event string) ([]HistoryItem, error) {
	rows, err := s.DB.Query(ctx, `SELECT id::text,order_id,action,version,created_at FROM (
	 SELECT a.id,a.order_id,a.action,a.version,a.created_at FROM core.order_audit a
	 JOIN core.orders o ON o.id=a.order_id WHERE o.owner=$1 AND o.event_id=$2 AND a.snapshot IS NOT NULL
	 ORDER BY a.id DESC LIMIT 30) recent ORDER BY recent.id`, owner, event)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[HistoryItem])
	return items, core.DatabaseOperationError(err)
}

// HistoryPage includes legacy metadata and deleted orders, using a stable audit ID.
func (s Service) HistoryPage(ctx context.Context, owner, event, raw string) (core.ReadPage[HistoryItem], error) {
	cursor, err := core.DecodeReadCursor(raw, owner, "orders.history:"+event)
	if err != nil {
		return core.ReadPage[HistoryItem]{}, err
	}
	var before int64
	if cursor.Position != "" {
		before, err = strconv.ParseInt(cursor.Position, 10, 64)
		if err != nil || before <= 0 {
			return core.ReadPage[HistoryItem]{}, core.ReadProblem("read_cursor_invalid")
		}
	}
	rows, err := s.DB.Query(ctx, `SELECT a.id::text,o.id,a.action,a.version,a.created_at FROM core.order_audit a
	 JOIN core.orders o ON o.id=a.order_id WHERE o.owner=$1 AND o.event_id=$2
	 AND ($3::bigint=0 OR a.id<$3) ORDER BY a.id DESC LIMIT $4`, owner, event, before, core.ReadPageItems+1)
	if err != nil {
		return core.ReadPage[HistoryItem]{}, core.DatabaseOperationError(err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[HistoryItem])
	if err != nil {
		return core.ReadPage[HistoryItem]{}, core.DatabaseOperationError(err)
	}
	return core.NavigationPage(items, cursor, func(item HistoryItem) string { return item.ID })
}

func (s Service) HistoryDetail(ctx context.Context, owner, event, id, raw string) (core.ReadChunk, error) {
	return s.historyDetail(ctx, owner, event, id, raw, false)
}

func (s Service) HistoryTransportDetail(ctx context.Context, owner, event, id, raw string) (core.ReadChunk, error) {
	return s.historyDetail(ctx, owner, event, id, raw, true)
}

func (s Service) historyDetail(
	ctx context.Context,
	owner, event, id, raw string,
	transport bool,
) (core.ReadChunk, error) {
	cursor, err := core.DecodeReadCursor(raw, owner, "orders.history.read:"+event+":"+id)
	if err != nil {
		return core.ReadChunk{}, err
	}
	var detail struct {
		HistoryItem

		Change           *Change `json:"change"`
		DetailsAvailable bool    `json:"details_available"`
	}
	err = core.DatabaseOperationError(
		s.DB.QueryRow(ctx, `SELECT a.id::text,o.id,a.action,a.version,a.created_at,a.snapshot FROM core.order_audit a JOIN core.orders o ON o.id=a.order_id
	 WHERE o.owner=$1 AND o.event_id=$2 AND a.id::text=$3`, owner, event, id).
			Scan(&detail.ID, &detail.OrderID, &detail.Action, &detail.Version, &detail.At, &detail.Change),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ReadChunk{}, problem(http.StatusNotFound, "history_not_found")
	}
	if err != nil {
		return core.ReadChunk{}, err
	}
	detail.DetailsAvailable = detail.Change != nil
	if detail.Change != nil {
		detail.Change.At = detail.At
	}
	// Repeated day/meal names can expand a bounded canonical choice into a
	// larger immutable audit snapshot. Keep every detail while bounding each page.
	data, err := json.Marshal(detail)
	if err != nil {
		return core.ReadChunk{}, err
	}
	if transport {
		return core.JSONReadTransportChunk(data, cursor)
	}
	return core.JSONReadDataChunk(data, cursor)
}

func (s Service) ReviewOrder(ctx context.Context, actor, event, id string) (Order, error) {
	if err := s.authorizeInbox(ctx, actor, event); err != nil {
		return Order{}, err
	}
	order, err := scan(
		s.DB.QueryRow(
			ctx,
			`SELECT `+columns+` FROM core.orders WHERE event_id=$1 AND id=$2 AND state IN ('proof','cash')`,
			event,
			id,
		),
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, problem(http.StatusNotFound, "order_not_found")
	}
	return order, err
}

// EventsPage lists the public catalog, matching the existing authenticated event read.
func (s Service) EventsPage(ctx context.Context, actor, raw string) (core.ReadPage[string], error) {
	cursor, err := core.DecodeReadCursor(raw, actor, "orders.events")
	if err != nil {
		return core.ReadPage[string]{}, err
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT id FROM core.order_events WHERE id>$1 ORDER BY id LIMIT $2`,
		cursor.Position,
		core.ReadPageItems+1,
	)
	if err != nil {
		return core.ReadPage[string]{}, core.DatabaseOperationError(err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return core.ReadPage[string]{}, core.DatabaseOperationError(err)
	}
	return core.NavigationPage(items, cursor, func(id string) string { return id })
}
