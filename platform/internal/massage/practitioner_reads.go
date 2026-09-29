package massage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type PractitionerWork struct {
	ID    int64     `json:"id"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type practitionerBoundary struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func practitionerCursor(raw, actor, kind, event, party string) (core.ReadCursor, practitionerBoundary, error) {
	// A fixed-size scope preserves exact filter binding without re-escaping IDs
	// into an oversized continuation token.
	scope, _ := json.Marshal([]string{kind, actor, event, party})
	scopeDigest := sha256.Sum256(scope)
	cursor, err := core.DecodeReadCursor(raw, actor, hex.EncodeToString(scopeDigest[:]))
	var boundary practitionerBoundary
	if err != nil {
		return cursor, boundary, err
	}
	if cursor.Position != "" &&
		(json.Unmarshal([]byte(cursor.Position), &boundary) != nil || boundary.At.IsZero() || boundary.ID == "") {
		return cursor, boundary, core.ReadProblem("read_cursor_invalid")
	}
	return cursor, boundary, nil
}

func practitionerPosition(at time.Time, id string) string {
	raw, _ := json.Marshal(practitionerBoundary{At: at, ID: id})
	return string(raw)
}

func (s Service) PractitionerSchedule(
	ctx context.Context,
	actor, event, raw string,
) (core.ReadPage[PractitionerWork], error) {
	cursor, boundary, err := practitionerCursor(raw, actor, "massage.practitioner.schedule", event, "")
	if err != nil {
		return core.ReadPage[PractitionerWork]{}, err
	}
	var id int64
	if cursor.Position != "" {
		id, err = strconv.ParseInt(boundary.ID, 10, 64)
		if err != nil || id <= 0 {
			return core.ReadPage[PractitionerWork]{}, core.ReadProblem("read_cursor_invalid")
		}
	}
	tx, err := practitionerReadTx(ctx, s, actor, event)
	if err != nil {
		return core.ReadPage[PractitionerWork]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT id,starts_at,ends_at FROM core.massage_work
 WHERE event_id=$1 AND specialist=$2 AND ($3='' OR (starts_at,id)>($4,$5)) ORDER BY starts_at,id LIMIT $6`, event, actor, cursor.Position, boundary.At, id, core.ReadPageItems+1)
	if err != nil {
		return core.ReadPage[PractitionerWork]{}, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PractitionerWork])
	if err != nil {
		return core.ReadPage[PractitionerWork]{}, err
	}
	page, err := core.NavigationPage(items, cursor, func(item PractitionerWork) string {
		return practitionerPosition(item.Start, strconv.FormatInt(item.ID, 10))
	})
	if err != nil {
		return page, err
	}
	return page, tx.Commit(ctx)
}

func (s Service) PractitionerBookings(
	ctx context.Context,
	actor, event, party, raw string,
) (core.ReadPage[Reservation], error) {
	cursor, boundary, err := practitionerCursor(raw, actor, "massage.practitioner.bookings", event, party)
	if err != nil {
		return core.ReadPage[Reservation]{}, err
	}
	tx, err := practitionerReadTx(ctx, s, actor, event)
	if err != nil {
		return core.ReadPage[Reservation]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT `+reservationColumns+` FROM core.massage_bookings
 WHERE event_id=$1 AND specialist=$2 AND cancelled_at IS NULL AND ($3='' OR party_id=$3)
 AND ($4='' OR (starts_at,id)>($5,$6)) ORDER BY starts_at,id LIMIT $7`, event, actor, party, cursor.Position, boundary.At, boundary.ID, core.ReadPageItems+1)
	if err != nil {
		return core.ReadPage[Reservation]{}, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Reservation])
	if err != nil {
		return core.ReadPage[Reservation]{}, err
	}
	page, err := core.NavigationPage(
		items,
		cursor,
		func(item Reservation) string { return practitionerPosition(item.Start, item.ID) },
	)
	if err != nil {
		return page, err
	}
	return page, tx.Commit(ctx)
}
