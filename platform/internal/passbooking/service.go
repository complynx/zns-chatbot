package passbooking

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func hash(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func validate(c Command) error {
	const maxKey = 200
	if c.QueueInvitation && c.Name != commandInvite {
		return invalid()
	}
	if c.Event == "" || len(c.Event) > maxKey || c.Key == "" || len(c.Key) > maxKey || c.Version < 0 ||
		c.TargetVersion < 0 ||
		c.InviteTelegramID < 0 ||
		strings.ContainsRune(c.Key, 0) {
		return invalid()
	}
	switch c.Name {
	case "solo",
		commandInvite,
		commandAccept,
		commandDecline,
		"cancel",
		"payment_admin",
		CommandTakeover, CommandReceivedOnly,
		commandAdminCancel,
		"admin_uncouple",
		commandProof, commandProofAccept, commandProofReject,
		commandRecalculate:
	default:
		return invalid()
	}
	return nil
}

// Execute locks the event before reading applications. Pair mutations and queue
// assignment commit together; failed work leaves no idempotency marker.
func (s Service) Execute(ctx context.Context, actor string, c Command) (Booking, error) {
	if err := validate(c); err != nil {
		return Booking{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Booking{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := s.executeInTx(ctx, tx, actor, c)
	if err != nil {
		return Booking{}, err
	}
	return result, tx.Commit(ctx)
}

// executeInTx retains the shared command semantics for durable transactional adapters.
func (s Service) executeInTx(ctx context.Context, tx pgx.Tx, actor string, c Command) (Booking, error) {
	e, err := readEvent(ctx, tx, c.Event)
	if err != nil {
		return Booking{}, err
	}
	telegramID, err := authorize(ctx, tx, actor, c.Name, c.Event)
	if err != nil {
		return Booking{}, err
	}
	if c.QueueInvitation {
		// Queue reads require the booking-admin grant. Reuse its exact predicate
		// and hold the grant's share lock through replay checking and effects.
		if _, err = authorize(ctx, tx, actor, commandAdminAssign, c.Event); err != nil {
			return Booking{}, err
		}
	}
	records, err := readBookings(ctx, tx, c.Event)
	if err != nil {
		return Booking{}, err
	}
	current := records[actor]
	if current == nil {
		current = &Booking{Event: c.Event, Owner: actor, TelegramID: telegramID}
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return Booking{}, err
	}
	keyHash, requestHash := hash([]byte(c.Key)), hash(encoded)
	var previous string
	err = tx.QueryRow(ctx, `SELECT request_hash FROM core.pass_booking_operations WHERE event_id=$1 AND actor=$2 AND key_hash=$3`, c.Event, actor, keyHash).
		Scan(&previous)
	if err == nil {
		if previous != requestHash {
			return Booking{}, conflict("idempotency_conflict")
		}
		return *current, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Booking{}, err
	}
	if current.Version != c.Version {
		return Booking{}, conflict("pass_booking_stale")
	}
	if err = lockRegistrationProfile(ctx, tx, actor, c.Name, e.passport); err != nil {
		return Booking{}, err
	}
	var now time.Time
	// Transaction start can precede a long lock wait. All decisions use the clock
	// after event, permission and required profile locks have been acquired.
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return Booking{}, err
	}
	state := newSnapshot(e, records, now)
	before := copyBookings(records)
	if err = state.mutate(ctx, tx, current, c); err != nil {
		return Booking{}, err
	}
	if now.Before(e.finishes) && c.Name != CommandTakeover && c.Name != CommandReceivedOnly {
		state.allocate()
	}
	if err = state.persistNotified(ctx, tx, before, c.Name); err != nil {
		return Booking{}, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_booking_operations(event_id,actor,key_hash,request_hash) VALUES($1,$2,$3,$4)`,
		c.Event,
		actor,
		keyHash,
		requestHash,
	)
	if err != nil {
		return Booking{}, err
	}
	return *current, nil
}

func lockRegistrationProfile(ctx context.Context, tx pgx.Tx, actor, name string, passport bool) error {
	if name != solo && name != commandInvite && (name != commandAccept || !passport) {
		return nil
	}
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_profiles WHERE owner=$1 FOR SHARE`, actor).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return conflict("pass_profile_required")
	}
	return err
}

func authorize(ctx context.Context, tx pgx.Tx, actor, name, eventID string) (int64, error) {
	var telegramID int64
	var canBook bool
	err := tx.QueryRow(ctx, `SELECT telegram_id,can_book FROM core.users WHERE id=$1 FOR SHARE`, actor).
		Scan(&telegramID, &canBook)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, forbidden()
	}
	if err != nil {
		return 0, err
	}
	if name == CommandTakeover || name == CommandReceivedOnly {
		return telegramID, authorizeTakeover(ctx, tx, actor, eventID)
	}
	if name == commandProofAccept || name == commandProofReject {
		var admin string
		err = tx.QueryRow(ctx, `SELECT owner FROM core.pass_payment_admins WHERE event_id=$1 AND owner=$2 FOR SHARE`, eventID, actor).
			Scan(&admin)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, forbidden()
		}
		return telegramID, err
	}
	if name == commandAdminCancel {
		return telegramID, authorizeBatchCancel(ctx, tx, actor, eventID)
	}
	if strings.HasPrefix(name, "admin_") || name == commandRecalculate {
		var admin string
		err = tx.QueryRow(ctx, `SELECT owner FROM core.pass_booking_admins WHERE owner=$1 FOR SHARE`, actor).
			Scan(&admin)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, forbidden()
		}
		return telegramID, err
	}
	if !canBook {
		return 0, forbidden()
	}
	return telegramID, nil
}
