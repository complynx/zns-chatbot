package massage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s Service) Execute(ctx context.Context, actor string, command Command) (Reservation, error) {
	if len(command.Key) < 1 || len(command.Key) > 128 || strings.TrimSpace(command.Event) == "" {
		return Reservation{}, problem(http.StatusBadRequest, "invalid_command")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Reservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = authenticated(ctx, tx, actor); err != nil {
		return Reservation{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('massage:'||$1,0))`, actor); err != nil {
		return Reservation{}, err
	}
	raw, err := json.Marshal(command)
	if err != nil {
		return Reservation{}, err
	}
	hash := sha256.Sum256(raw)
	fingerprint := hex.EncodeToString(hash[:])
	var previous string
	var result Reservation
	err = tx.QueryRow(ctx, `SELECT request_hash,result FROM core.massage_operations WHERE actor=$1 AND key=$2`, actor, command.Key).
		Scan(&previous, &result)
	if err == nil {
		if previous != fingerprint {
			return result, problem(http.StatusConflict, "idempotency_conflict")
		}
		return result, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	switch command.Action {
	case "book", actionInstant:
		result, err = s.book(ctx, tx, actor, command)
	case "cancel":
		result, err = s.cancel(ctx, tx, actor, command)
	default:
		return result, problem(http.StatusBadRequest, "invalid_action")
	}
	if err != nil {
		return result, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.massage_operations(actor,key,request_hash,result) VALUES($1,$2,$3,$4)`,
		actor,
		command.Key,
		fingerprint,
		result,
	)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func availability(
	party EventParty,
	staff []Provider,
	reservations []Reservation,
	actor string,
	length int,
	now time.Time,
) (Slots, []Booking, bool, error) {
	specialists := make([]Specialist, 0, len(staff))
	ids := make(map[string]int64, len(staff))
	isStaff := false
	for _, provider := range staff {
		ids[provider.Owner] = provider.ID
		isStaff = isStaff || provider.Owner == actor
		specialists = append(
			specialists,
			Specialist{ID: provider.ID, LegacyTableFlag: provider.LegacyTableFlag, Work: provider.Work},
		)
	}
	bookings, own := make([]Booking, 0, len(reservations)), []Booking{}
	for _, reservation := range reservations {
		booking := Booking{
			SpecialistID: ids[reservation.Specialist],
			Slot:         reservation.Slot,
			Length:       reservation.Length,
		}
		bookings = append(bookings, booking)
		if reservation.Owner == actor {
			own = append(own, booking)
		}
	}
	slots, err := Available(party.rules(), specialists, bookings, length, now)
	if errors.Is(err, ErrTables) {
		err = problem(http.StatusConflict, "unsupported_table_configuration")
	}
	return slots, own, isStaff, err
}

func (s Service) book(ctx context.Context, tx pgx.Tx, actor string, command Command) (Reservation, error) {
	party, err := loadParty(ctx, tx, command.Event, command.Party, true)
	if err != nil {
		return Reservation{}, err
	}
	staff, existing, err := snapshot(ctx, tx, command.Event, command.Party)
	if err != nil {
		return Reservation{}, err
	}
	now := s.now()
	if err = currentInstantParty(ctx, tx, command, now); err != nil {
		return Reservation{}, err
	}
	command, err = prepareBooking(command, party, staff, actor, now)
	if err != nil {
		return Reservation{}, err
	}
	if command.ExpectedStart != nil && !SlotTime(party.rules(), command.Slot).Equal(*command.ExpectedStart) {
		return Reservation{}, problem(http.StatusConflict, "stale_slot")
	}
	slots, own, isStaff, err := availability(party, staff, existing, actor, command.Length, now)
	if err != nil {
		return Reservation{}, err
	}
	var selected Provider
	for _, provider := range staff {
		if provider.Owner == command.Specialist {
			selected = provider
		}
	}
	if selected.Owner == "" ||
		(command.Action != actionInstant && (command.Length < selected.MinLength || command.Length > selected.MaxLength)) {
		return Reservation{}, problem(http.StatusConflict, "specialist_unavailable")
	}
	decision := Eligibility(
		party.rules(),
		now,
		Request{Slot: command.Slot, Length: command.Length, SpecialistID: selected.ID,
			ActorIsSpecialist: isStaff, DailyLimit: party.DailyLimit},
		slots,
		own,
	)
	if decision != Allowed {
		return Reservation{}, problem(http.StatusConflict, string(decision))
	}
	duration, price, err := Quote(command.Length, BYN)
	if err != nil {
		return Reservation{}, err
	}
	_, rub, err := Quote(command.Length, RUB)
	if err != nil {
		return Reservation{}, err
	}
	result := Reservation{
		ID:         uuid.NewString(),
		Event:      command.Event,
		Party:      command.Party,
		Owner:      actor,
		Specialist: selected.Owner,
		Slot:       command.Slot,
		Length:     command.Length,
		Start:      SlotTime(party.rules(), command.Slot),
		Price:      price,
		PriceRUB:   rub,
		Version:    1,
		Instant:    command.Action == actionInstant,
	}
	result.End = result.Start.Add(duration)
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.massage_bookings(id,event_id,party_id,owner,specialist,slot,length,starts_at,ends_at,price,price_rub,instant)
	VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		result.ID,
		result.Event,
		result.Party,
		result.Owner,
		result.Specialist,
		result.Slot,
		result.Length,
		result.Start,
		result.End,
		result.Price,
		result.PriceRUB,
		result.Instant,
	)
	if err != nil {
		return result, err
	}
	if selected.NotifyBookings && !result.Instant {
		err = queueNotice(ctx, tx, result.ID, selected.Owner, "booked")
	}
	return result, err
}

func prepareBooking(command Command, party EventParty, staff []Provider, actor string, now time.Time) (Command, error) {
	if command.Action == "book" {
		if !regularLength(command.Length) || party.Open {
			return command, problem(http.StatusBadRequest, "invalid_length_or_party")
		}
		return command, nil
	}
	if command.Length < 1 || command.Length > 6 {
		return command, problem(http.StatusBadRequest, "invalid_length")
	}
	for _, provider := range staff {
		if provider.Owner != actor {
			continue
		}
		if _, ok := CurrentParty([]Party{party.rules()}, now); !ok {
			return command, problem(http.StatusConflict, "no_current_party")
		}
		command.Specialist = actor
		command.Slot = InstantSlot(party.rules(), now)
		return command, nil
	}
	return command, problem(http.StatusForbidden, "forbidden")
}

func (s Service) cancel(ctx context.Context, tx pgx.Tx, actor string, command Command) (Reservation, error) {
	var partyID string
	err := tx.QueryRow(ctx, `SELECT party_id FROM core.massage_bookings WHERE id=$1 AND event_id=$2 AND owner=$3`, command.Booking, command.Event, actor).
		Scan(&partyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, problem(http.StatusNotFound, "not_found")
	}
	if err != nil {
		return Reservation{}, err
	}
	if _, err = loadParty(ctx, tx, command.Event, partyID, true); err != nil {
		return Reservation{}, err
	}
	rows, err := tx.Query(
		ctx,
		`SELECT `+reservationColumns+` FROM core.massage_bookings WHERE id=$1 FOR UPDATE`,
		command.Booking,
	)
	if err != nil {
		return Reservation{}, err
	}
	result, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Reservation])
	if err != nil {
		return result, err
	}
	if result.Version != command.Version {
		return result, problem(http.StatusConflict, "stale_version")
	}
	if result.CancelledAt != nil {
		return result, nil
	}
	now := s.now()
	result.CancelledAt = &now
	result.Version++
	_, err = tx.Exec(
		ctx,
		`UPDATE core.massage_bookings SET cancelled_at=$2,version=$3 WHERE id=$1`,
		result.ID,
		now,
		result.Version,
	)
	if err != nil {
		return result, err
	}
	var notify bool
	err = tx.QueryRow(ctx, `SELECT notify_bookings FROM core.massage_specialists WHERE event_id=$1 AND owner=$2`, command.Event, result.Specialist).
		Scan(&notify)
	if err != nil {
		return result, err
	}
	if notify {
		err = queueNotice(ctx, tx, result.ID, result.Specialist, "cancelled")
	}
	return result, err
}

func queueNotice(ctx context.Context, tx pgx.Tx, booking, owner, kind string) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.massage_notices(booking_id,owner,kind) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`,
		booking,
		owner,
		kind,
	)
	return err
}

const actionInstant = "instant"

func currentInstantParty(ctx context.Context, tx pgx.Tx, command Command, now time.Time) error {
	if command.Action != actionInstant {
		return nil
	}
	var current string
	err := tx.QueryRow(ctx, `SELECT id FROM core.massage_parties WHERE event_id=$1
 AND starts_at-interval '2 hours'<$2 AND ends_at+interval '2 hours'>$2
 ORDER BY position,starts_at,id LIMIT 1`, command.Event, now).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && current != command.Party) {
		return problem(http.StatusConflict, "no_current_party")
	}
	return err
}
