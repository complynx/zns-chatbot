package massage

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

const legacyToggleBookings = 51

func (s Service) LegacyPractitionerBooking(ctx context.Context, actor, event, id string) (Reservation, error) {
	tx, err := practitionerReadTx(ctx, s, actor, event)
	if err != nil {
		return Reservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT `+reservationColumns+` FROM core.massage_bookings WHERE id=(
 SELECT target_id FROM core.legacy_massage_import_references WHERE source_kind='booking' AND event_id=$1
 AND (target_id=$3 OR source_record->'_id'->>'$oid'=$3)) AND event_id=$1 AND specialist=$2`, event, actor, id)
	if err != nil {
		return Reservation{}, core.DatabaseOperationError(err)
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Reservation])
	if errors.Is(err, pgx.ErrNoRows) {
		return result, problem(http.StatusNotFound, "not_found")
	}
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

// LegacyTogglePreferences retains the source toggle intent once per delivered
// update, while current role membership remains locked through the shared write.
func (s Service) LegacyTogglePreferences(
	ctx context.Context,
	actor, event, key string,
	choice int,
) (Preferences, error) {
	if key == "" || len(key) > 100 || (choice != 51 && choice != 52) {
		return Preferences{}, problem(http.StatusBadRequest, "invalid_command")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Preferences{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('massage:'||$1,0))`, actor); err != nil {
		return Preferences{}, core.DatabaseOperationError(err)
	}
	var value Preferences
	err = tx.QueryRow(ctx, `SELECT notify_bookings,notify_next FROM core.massage_specialists WHERE event_id=$1 AND owner=$2 FOR UPDATE`, event, actor).
		Scan(&value.Bookings, &value.Next)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, problem(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return value, core.DatabaseOperationError(err)
	}
	fingerprint := event + ":" + strconv.Itoa(choice)
	var prior string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM core.massage_operations WHERE actor=$1 AND key=$2`, actor, "legacy-pref:"+key).
		Scan(&prior)
	if err == nil {
		if prior != fingerprint {
			return value, problem(http.StatusConflict, "idempotency_conflict")
		}
		return value, core.DatabaseOperationError(tx.Commit(ctx))
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return value, core.DatabaseOperationError(err)
	}
	if choice == legacyToggleBookings {
		value.Bookings = !value.Bookings
	} else {
		value.Next = !value.Next
	}
	if _, err = setPreferences(ctx, tx, actor, event, value); err != nil {
		return value, err
	}
	if _, err = tx.Exec(
		ctx,
		`INSERT INTO core.massage_operations(actor,key,request_hash,result) VALUES($1,$2,$3,$4)`,
		actor,
		"legacy-pref:"+key,
		fingerprint,
		value,
	); err != nil {
		return value, core.DatabaseOperationError(err)
	}
	return value, core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) LegacyInstant(ctx context.Context, actor, event, key string, length int) (Reservation, error) {
	if key == "" || len(key) > 100 {
		return Reservation{}, problem(http.StatusBadRequest, "invalid_command")
	}
	if _, err := s.Preferences(ctx, actor, event); err != nil {
		return Reservation{}, err
	}
	operationKey := "legacy-instant:" + key
	var party string
	err := s.DB.QueryRow(ctx, `SELECT result->>'party' FROM core.massage_operations WHERE actor=$1 AND key=$2`, actor, operationKey).
		Scan(&party)
	if errors.Is(err, pgx.ErrNoRows) {
		err = s.DB.QueryRow(ctx, `SELECT id FROM core.massage_parties WHERE event_id=$1 AND starts_at-interval '2 hours'<$2 AND ends_at+interval '2 hours'>$2 ORDER BY position,starts_at,id LIMIT 1`, event, s.now()).
			Scan(&party)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, problem(http.StatusConflict, "no_current_party")
	}
	if err != nil {
		return Reservation{}, core.DatabaseOperationError(err)
	}
	return s.Execute(
		ctx,
		actor,
		Command{Event: event, Party: party, Action: actionInstant, Key: operationKey, Length: length},
	)
}
