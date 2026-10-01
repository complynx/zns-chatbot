package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

// AdminAssign serializes forced assignments with normal event registration.
// A repeated key returns current records; it cannot apply price or capacity twice.
func (s Service) AdminAssign(ctx context.Context, actor string, c AdminAssignment) (AdminAssignmentResult, error) {
	if err := s.ResolveRegistrationIntake(ctx, c.Event); err != nil {
		return AdminAssignmentResult{}, err
	}
	if err := validateAdminAssignment(c); err != nil {
		return AdminAssignmentResult{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return AdminAssignmentResult{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	result, err := s.adminAssignInTx(ctx, tx, actor, c)
	if err != nil {
		return AdminAssignmentResult{}, clockAttempt.DecisionError(ctx, err)
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return AdminAssignmentResult{}, err
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

// adminAssignInTx lets durable adapters commit the result marker with the command.
func (s Service) adminAssignInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c AdminAssignment,
) (AdminAssignmentResult, error) {
	p, err := s.PrepareAssignmentInTx(ctx, tx, actor, c)
	if err != nil {
		return AdminAssignmentResult{}, err
	}
	return p.Apply(ctx)
}

type PreparedAssignment struct {
	registrationClock     registrationingress.Clock
	registrationRetention time.Duration
	deliveryBotID         int64
	announcementBindings  *destination.Bindings
	tx                    pgx.Tx
	actor                 string
	command               AdminAssignment
	event                 event
	records               map[string]*Booking
	keyHash               string
	requestHash           string
	replay                AdminAssignmentResult
	found                 bool
}

func (s Service) PrepareAssignmentInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c AdminAssignment,
) (*PreparedAssignment, error) {
	if err := validateAdminAssignment(c); err != nil {
		return nil, err
	}
	e, err := readEvent(ctx, tx, c.Event)
	if err != nil {
		return nil, err
	}
	if err = lockAdminUsers(ctx, tx, actor, c.Target); err != nil {
		return nil, err
	}
	if _, err = authorize(ctx, tx, actor, commandAdminAssign, c.Event); err != nil {
		return nil, err
	}
	records, err := readBookings(ctx, tx, c.Event)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	keyHash, requestHash := hash([]byte(c.Key)), hash(encoded)
	replay, found, err := adminReplay(ctx, tx, actor, c, keyHash, requestHash, records)
	if err != nil {
		return nil, err
	}
	return &PreparedAssignment{
		registrationRetention: s.registrationRetention(),
		registrationClock:     s.RegistrationClock,
		deliveryBotID:         s.Delivery.BotID,
		announcementBindings:  s.AnnouncementBindings,
		tx:                    tx,
		actor:                 actor,
		command:               c,
		event:                 e,
		records:               records,
		keyHash:               keyHash,
		requestHash:           requestHash,
		replay:                replay,
		found:                 found,
	}, nil
}

func (p *PreparedAssignment) Replay() (AdminAssignmentResult, bool) { return p.replay, p.found }

func (p *PreparedAssignment) Apply(ctx context.Context) (AdminAssignmentResult, error) {
	if p.found {
		return p.replay, nil
	}
	tx, actor, c, e, records := p.tx, p.actor, p.command, p.event, p.records
	if bookingVersion(records[actor]) != c.Version || bookingVersion(records[c.Target]) != c.TargetVersion {
		return AdminAssignmentResult{}, conflict("pass_booking_stale")
	}
	profile, err := lockAdminProfile(ctx, tx, c)
	if err != nil {
		return AdminAssignmentResult{}, err
	}
	now, err := registrationTurnTime(ctx, tx, p.registrationClock)
	if err != nil {
		return AdminAssignmentResult{}, err
	}
	if !now.Before(e.finishes) {
		return AdminAssignmentResult{}, conflict("pass_event_finished")
	}
	before := copyBookings(records)
	if err = refreshRegistrationTurns(ctx, tx, c.Event, p.registrationRetention, now); err != nil {
		return AdminAssignmentResult{}, err
	}
	state := newSnapshot(e, records, now)
	if p.registrationClock != nil {
		state.registrationObserved = &now
	}
	if err = state.loadRegistrationRanks(ctx, tx); err != nil {
		return AdminAssignmentResult{}, err
	}
	state.deliveryBotID = p.deliveryBotID
	state.announcementBindings = p.announcementBindings
	targets, err := state.adminTargets(ctx, tx, c, profile)
	if err != nil {
		return AdminAssignmentResult{}, err
	}
	if err = state.adminPrices(c, targets); err != nil {
		return AdminAssignmentResult{}, err
	}
	if err = state.appendAdminTier(ctx, tx, c, len(targets), before); err != nil {
		return AdminAssignmentResult{}, err
	}
	state.allocate()
	return state.persistAdmin(ctx, tx, actor, c, targets, before, p.keyHash, p.requestHash)
}

func (s *snapshot) persistAdmin(ctx context.Context, tx pgx.Tx, actor string, c AdminAssignment,
	targets []*Booking, before map[string]*Booking, keyHash, requestHash string) (AdminAssignmentResult, error) {
	if err := persist(ctx, tx, s); err != nil {
		return AdminAssignmentResult{}, err
	}
	if err := s.adminPayments(ctx, tx, actor, c, targets, before); err != nil {
		return AdminAssignmentResult{}, err
	}
	if err := s.notifyChanges(ctx, tx, before, commandAdminAssign); err != nil {
		return AdminAssignmentResult{}, err
	}
	result := s.adminResult(len(targets))
	if err := recordAdminAssignment(ctx, tx, actor, c, keyHash, requestHash, before, result, s.now); err != nil {
		return AdminAssignmentResult{}, err
	}
	return result, nil
}

func bookingVersion(b *Booking) int64 {
	if b == nil {
		return 0
	}
	return b.Version
}

func copyBookings(records map[string]*Booking) map[string]*Booking {
	result := make(map[string]*Booking, len(records))
	for owner, b := range records {
		copied := *b
		result[owner] = &copied
	}
	return result
}

func lockAdminUsers(ctx context.Context, tx pgx.Tx, actor, target string) error {
	rows, err := tx.Query(
		ctx,
		`SELECT id FROM core.users WHERE id=ANY($1) ORDER BY id FOR UPDATE`,
		[]string{actor, target},
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	owners, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if !slices.Contains(owners, actor) || !slices.Contains(owners, target) {
		return forbidden()
	}
	return nil
}

func adminReplay(ctx context.Context, tx pgx.Tx, actor string, c AdminAssignment, keyHash, requestHash string,
	records map[string]*Booking) (AdminAssignmentResult, bool, error) {
	var previous string
	err := tx.QueryRow(ctx, `SELECT request_hash FROM core.pass_booking_operations WHERE event_id=$1 AND actor=$2 AND key_hash=$3`, c.Event, actor, keyHash).
		Scan(&previous)
	if errors.Is(err, pgx.ErrNoRows) {
		return AdminAssignmentResult{}, false, nil
	}
	if err != nil {
		return AdminAssignmentResult{}, false, core.DatabaseOperationError(err)
	}
	if previous != requestHash {
		return AdminAssignmentResult{}, false, conflict("idempotency_conflict")
	}
	var result AdminAssignmentResult
	var prior []byte
	err = tx.QueryRow(ctx, `SELECT assigned_count,after_records FROM core.pass_admin_assignments WHERE event_id=$1 AND actor=$2 AND key_hash=$3`, c.Event, actor, keyHash).
		Scan(&result.AssignedCount, &prior)
	if err != nil {
		return result, false, core.DatabaseOperationError(err)
	}
	var old []Booking
	if err = json.Unmarshal(prior, &old); err != nil {
		return result, false, err
	}
	result.Bookings = []Booking{}
	for _, b := range old {
		if current := records[b.Owner]; current != nil {
			result.Bookings = append(result.Bookings, *current)
		}
	}
	return result, true, nil
}

func (s *snapshot) adminResult(count int) AdminAssignmentResult {
	result := AdminAssignmentResult{AssignedCount: count, Bookings: []Booking{}}
	for owner := range s.dirty {
		result.Bookings = append(result.Bookings, *s.bookings[owner])
	}
	slices.SortFunc(result.Bookings, func(a, b Booking) int { return s.ordered(&a, &b) })
	return result
}

func recordAdminAssignment(ctx context.Context, tx pgx.Tx, actor string, c AdminAssignment, keyHash, requestHash string,
	before map[string]*Booking, result AdminAssignmentResult, now time.Time) error {
	previous := []Booking{}
	for _, b := range result.Bookings {
		if old := before[b.Owner]; old != nil {
			previous = append(previous, *old)
		}
	}
	prior, err := json.Marshal(previous)
	if err != nil {
		return err
	}
	after, err := json.Marshal(result.Bookings)
	if err != nil {
		return err
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
		return core.DatabaseOperationError(err)
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_admin_assignments(event_id,actor,key_hash,target,assigned_count,before_records,after_records,at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		c.Event,
		actor,
		keyHash,
		c.Target,
		result.AssignedCount,
		prior,
		after,
		now,
	)
	return core.DatabaseOperationError(err)
}
