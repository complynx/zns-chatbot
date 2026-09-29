package orders

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
)

const orderPageSize = 25
const orderPageBytes = 512 << 10
const maxChoiceBytes = 256 << 10

type Page struct {
	Orders []Order `json:"orders"`
	Next   string  `json:"next"`
}

func (s Service) Get(ctx context.Context, owner, event, id string) (Order, error) {
	order, err := scan(s.DB.QueryRow(ctx, `SELECT `+columns+` FROM core.orders
		WHERE owner=$1 AND event_id=$2 AND id=$3 AND state<>'deleted'`, owner, event, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, problem(http.StatusNotFound, "order_not_found")
	}
	return order, err
}

// ListPage uses stable creation order and a byte budget, including choices.
// The cursor order is retained after deletion, so it remains a valid boundary.
func (s Service) ListPage(ctx context.Context, actor, event, cursor string, inbox bool) (Page, error) {
	if inbox {
		if err := s.authorizeInbox(ctx, actor, event); err != nil {
			return Page{}, err
		}
	}
	rows, err := s.DB.Query(ctx, `SELECT `+columns+` FROM core.orders
		WHERE event_id=$1 AND (($4 AND state IN ('proof','cash')) OR (NOT $4 AND owner=$2 AND state<>'deleted'))
		AND ($3='' OR (created_at,id)>(SELECT created_at,id FROM core.orders WHERE id=$3 AND event_id=$1 AND ($4 OR owner=$2)))
		ORDER BY created_at,id LIMIT $5`, event, actor, cursor, inbox, orderPageSize+1)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Orders: []Order{}}
	size := 0
	for rows.Next() {
		order, scanError := scan(rows)
		if scanError != nil {
			return Page{}, scanError
		}
		encoded, encodeError := json.Marshal(order)
		if encodeError != nil {
			return Page{}, encodeError
		}
		if len(page.Orders) > 0 && (len(page.Orders) == orderPageSize || size+len(encoded) > orderPageBytes) {
			page.Next = page.Orders[len(page.Orders)-1].ID
			break
		}
		if len(encoded) > orderPageBytes {
			return Page{}, problem(http.StatusRequestEntityTooLarge, "order_too_large")
		}
		page.Orders = append(page.Orders, order)
		size += len(encoded)
	}
	return page, rows.Err()
}

// GetByID uses the same owner visibility as Get without assuming the current event.
func (s Service) GetByID(ctx context.Context, owner, id string) (Order, error) {
	order, err := scan(s.DB.QueryRow(ctx, `SELECT `+columns+` FROM core.orders
 WHERE owner=$1 AND id=$2 AND state<>'deleted'`, owner, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, problem(http.StatusNotFound, "order_not_found")
	}
	return order, err
}
