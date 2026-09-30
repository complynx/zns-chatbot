package orders

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"
)

const refundAmbassadorAllowed = `b.state<>'cancelled' AND b.payment_admin<>'' AND ambassador.can_book
 AND (EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=b.payment_admin)
 OR EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=b.event_id AND a.owner=b.payment_admin))`

type refundQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// The pass booking's current ambassador is distinct from an order payment administrator.
func currentRefundAmbassador(ctx context.Context, q refundQuery, event, owner string) (string, string, error) {
	var assigned string
	var allowed bool
	err := core.DatabaseOperationError(
		q.QueryRow(ctx, `SELECT b.payment_admin,COALESCE((`+refundAmbassadorAllowed+`),false)
 FROM core.pass_bookings b LEFT JOIN core.users ambassador ON ambassador.id=b.payment_admin
 WHERE b.event_id=$1 AND b.owner=$2`, event, owner).Scan(&assigned, &allowed),
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && assigned == "") {
		return "", "ambassador_missing", nil
	}
	if err != nil {
		return "", "", err
	}
	if !allowed {
		return "", "ambassador_unavailable", nil
	}
	return assigned, "", nil
}

func authorizeRefundRead(ctx context.Context, q refundQuery, actor string, task *RefundTask) error {
	ambassador, reason, err := currentRefundAmbassador(ctx, q, task.EventID, task.Owner)
	if err != nil {
		return err
	}
	var active, globalAdmin bool
	err = core.DatabaseOperationError(
		q.QueryRow(ctx, `SELECT can_book,EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$1)
 FROM core.users WHERE id=$1`, actor).Scan(&active, &globalAdmin),
	)
	allowed := active && (actor == task.Owner || actor == ambassador || (ambassador == "" && globalAdmin))
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		return problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return err
	}
	task.Ambassador, task.RoutingReason = ambassador, reason
	task.CanConfirm = task.State == refundPending && actor == ambassador
	return nil
}

// Refund exposes one obligation only to its owner, current ambassador, or an existing global pass admin when unassigned.
func (s Service) Refund(ctx context.Context, actor string, id int64) (RefundTask, error) {
	if id <= 0 {
		return RefundTask{}, refundInvalid()
	}
	task, err := scanRefund(
		s.DB.QueryRow(ctx, `SELECT `+refundColumns+` FROM core.order_refund_tasks t WHERE t.id=$1`, id),
	)
	if err != nil {
		return task, refundReadError(err)
	}
	err = authorizeRefundRead(ctx, s.DB, actor, &task)
	if err != nil {
		return RefundTask{}, err
	}
	return task, nil
}

// RefundTasks uses an ID cursor and a fixed page size, including durable unassigned obligations.
func (s Service) RefundTasks(ctx context.Context, actor, event string, before int64) (RefundPage, error) {
	page := RefundPage{Items: []RefundTask{}}
	if before < 0 || len(event) > maxEventLength {
		return page, refundInvalid()
	}
	var active bool
	err := core.DatabaseOperationError(
		s.DB.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1`, actor).Scan(&active),
	)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !active) {
		return page, problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return page, err
	}
	rows, err := s.DB.Query(ctx, `SELECT t.id FROM core.order_refund_tasks t
 LEFT JOIN core.pass_bookings b ON b.event_id=t.event_id AND b.owner=t.owner
 LEFT JOIN core.users ambassador ON ambassador.id=b.payment_admin
 WHERE ($2='' OR t.event_id=$2) AND ($3::bigint=0 OR t.id<$3) AND
 (t.owner=$1 OR ((`+refundAmbassadorAllowed+`) AND b.payment_admin=$1)
 OR (NOT COALESCE((`+refundAmbassadorAllowed+`),false) AND EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=$1)))
 ORDER BY t.id DESC LIMIT $4`, actor, event, before, refundPageSize+1)
	if err != nil {
		return page, core.DatabaseOperationError(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	if err != nil {
		return page, core.DatabaseOperationError(err)
	}
	if len(ids) > refundPageSize {
		ids = ids[:refundPageSize]
		page.NextBefore = ids[len(ids)-1]
	}
	for _, id := range ids {
		task, readErr := s.Refund(ctx, actor, id)
		if readErr != nil {
			return RefundPage{}, readErr
		}
		page.Items = append(page.Items, task)
	}
	return page, nil
}
