package passbooking

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

const (
	admissionRegistered = "registered"
	admissionCanonical  = "canonical_ingress"
)

type Admission struct {
	ID         int64  `json:"id"`
	Generation int64  `json:"generation"`
	Position   int64  `json:"position"`
	State      string `json:"state"`
	SalesOpen  *bool  `json:"sales_open"`
	Origin     string `json:"origin"`
}

// AdmissionRequest is accepted only from authenticated trusted adapters.
type AdmissionRequest struct {
	Command Command                        `json:"command"`
	Ingress *registrationingress.Reference `json:"ingress,omitempty"`
}

func InitiatesRegistration(c Command) bool { return c.Name == solo || c.Name == commandInvite }

// CaptureAdmission commits the sales-open observation even when later profile
// completion or booking fails. The retained turn orders allocation, not capacity.
func (s Service) CaptureAdmission(ctx context.Context, actor string, request AdmissionRequest) (Admission, error) {
	if !InitiatesRegistration(request.Command) {
		return Admission{}, nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Admission{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	result, err := s.CaptureAdmissionInTx(ctx, tx, actor, request)
	if err != nil {
		return Admission{}, clockAttempt.DecisionError(ctx, err)
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return Admission{}, err
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

// CaptureAdmissionInTx validates current command authority before capture.
// Derived callers also hold their source and deletion fences until commit.
func (s Service) CaptureAdmissionInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	request AdmissionRequest,
) (Admission, error) {
	if !InitiatesRegistration(request.Command) || (request.Ingress != nil && !request.Ingress.Valid()) {
		return Admission{}, invalid()
	}
	p, err := s.PrepareAdmissionInTx(ctx, tx, actor, request.Command)
	if err != nil {
		return Admission{}, err
	}
	return p.Capture(ctx, request.Ingress)
}

func (p *PreparedCommand) captureAdmission(ctx context.Context, ref *registrationingress.Reference) (Admission, error) {
	var err error
	nativeTime, err := p.nativeAdmissionTime(ctx, ref)
	if !nativeTime.IsZero() {
		p.nativeReceivedAt = &nativeTime
	}
	if err != nil {
		return Admission{}, err
	}
	result, found, err := p.existingAdmission(ctx)
	if err != nil {
		return result, err
	}
	if found {
		return p.reconcileAdmission(ctx, result, ref)
	}
	if p.found {
		return Admission{}, nil
	}
	if p.current.Version != p.command.Version {
		return p.reconcileStaleAdmission(ctx, ref)
	}
	if !editable(p.current) {
		return Admission{}, conflict("pass_booking_state")
	}
	result, err = p.captureNewAdmission(ctx, ref)
	if err != nil {
		return Admission{}, err
	}
	_, err = p.tx.Exec(
		ctx,
		`INSERT INTO core.registration_intent_requests(event_id,owner,key_hash,request_hash,intent_id) VALUES($1,$2,$3,$4,$5)`,
		p.command.Event,
		p.actor,
		p.keyHash,
		p.requestHash,
		result.ID,
	)
	return result, core.DatabaseOperationError(err)
}

func (p *PreparedCommand) existingAdmission(ctx context.Context) (Admission, bool, error) {
	var result Admission
	var previous string
	err := p.tx.QueryRow(ctx, `SELECT r.request_hash,i.id,i.generation,COALESCE(i.ingress_id,0),i.state,i.sales_open,i.origin
 FROM core.registration_intent_requests r JOIN core.registration_intents i ON i.id=r.intent_id
 WHERE r.event_id=$1 AND r.owner=$2 AND r.key_hash=$3`, p.command.Event, p.actor, p.keyHash).
		Scan(&previous, &result.ID, &result.Generation, &result.Position, &result.State, &result.SalesOpen, &result.Origin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admission{}, false, nil
	}
	if err != nil {
		return Admission{}, false, core.DatabaseOperationError(err)
	}
	if previous != p.requestHash {
		return Admission{}, false, conflict("idempotency_conflict")
	}
	return result, true, nil
}

func (p *PreparedCommand) captureNewAdmission(
	ctx context.Context,
	ref *registrationingress.Reference,
) (Admission, error) {
	now, err := registrationTime(ctx, p.tx, p.registrationClock)
	if err != nil {
		return Admission{}, err
	}
	if !now.Before(p.event.finishes) {
		return Admission{}, conflict("pass_event_finished")
	}
	state := newSnapshot(p.event, p.records, now)
	var active Admission
	err = p.tx.QueryRow(ctx, `SELECT id,generation,COALESCE(ingress_id,0),state,sales_open,origin
 FROM core.registration_intents WHERE event_id=$1 AND owner=$2 AND state<>'cancelled'`, p.command.Event, p.actor).
		Scan(&active.ID, &active.Generation, &active.Position, &active.State, &active.SalesOpen, &active.Origin)
	if err == nil {
		return p.reconcileAdmission(ctx, active, ref)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Admission{}, core.DatabaseOperationError(err)
	}
	position, err := p.admissionPosition(ctx, ref)
	if err != nil {
		return Admission{}, err
	}
	if p.registrationClock != nil {
		now, err = registrationTime(ctx, p.tx, p.registrationClock)
		if err != nil {
			return Admission{}, err
		}
		if !now.Before(p.event.finishes) {
			return Admission{}, conflict("pass_event_finished")
		}
		state = newSnapshot(p.event, p.records, now)
		// Allocator waiting precedes the retained sales/turn decision. Original
		// ingress reception is immutable; the decision is rebuilt at this instant.
		if attempt, scoped := p.registrationClock.(*RegistrationClockAttempt); scoped {
			attempt.acceptCurrent(true)
		}
	}
	var retired bool
	if err = p.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.registration_intents WHERE event_id=$1 AND owner=$2 AND state='cancelled' AND closed_through_position >= $3)`, p.command.Event, p.actor, position).
		Scan(&retired); err != nil {
		return Admission{}, core.DatabaseOperationError(err)
	}
	if retired {
		return Admission{}, conflict("pass_admission_cancelled")
	}
	return p.insertAdmission(ctx, position, state.open(), now)
}

func (p *PreparedCommand) admissionPosition(ctx context.Context, ref *registrationingress.Reference) (int64, error) {
	if ref == nil {
		if p.registrationClock != nil {
			position, received, err := registrationingress.ApplicationObservation(
				registrationingress.WithClock(ctx, p.registrationClock), p.tx, p.actor, p.command.Event+":"+p.keyHash,
			)
			if err == nil {
				p.nativeReceivedAt = &received
			}
			return position, err
		}
		return registrationingress.ApplicationPosition(
			registrationingress.WithClock(ctx, p.registrationClock),
			p.tx,
			p.actor,
			p.command.Event+":"+p.keyHash,
		)
	}
	position, err := registrationingress.TelegramPosition(ctx, p.tx, *ref, p.current.TelegramID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, forbidden()
	}
	return position, err
}

func (p *PreparedCommand) insertAdmission(
	ctx context.Context,
	position int64,
	open bool,
	now time.Time,
) (Admission, error) {
	origin, state := admissionCanonical, "captured"
	var bookingTime *time.Time
	ingress := &position
	observedOpen := &open
	// Forward import may introduce historical bookings after this migration.
	// Keep their generation evidence labelled rather than assigning new priority.
	if p.current.Version > 0 && p.current.State != cancelled {
		origin, state = "legacy_fallback", admissionRegistered
		bookingTime = &p.current.CreatedAt
		ingress = nil
		observedOpen = nil
	}
	var result Admission
	// Preserve production insertion-time defaults. A configured clock stamps the
	// actual locked capture observation without changing historical evidence.
	var observed *time.Time
	if p.registrationClock != nil {
		observed = &now
	}
	err := p.tx.QueryRow(ctx, `INSERT INTO core.registration_intents(event_id,owner,generation,ingress_id,origin,state,sales_open,booking_created_at,effective_position,turn_expires_at,checked_at)
 SELECT $1,$2,COALESCE(MAX(generation),0)+1,$3,$4,$5,$6,$7,$3,
 CASE WHEN $4='canonical_ingress' THEN COALESCE($9::timestamptz,$10::timestamptz,clock_timestamp())+$8::bigint*interval '1 microsecond' ELSE NULL END,
 COALESCE($10::timestamptz,clock_timestamp())
 FROM core.registration_intents WHERE event_id=$1 AND owner=$2
 RETURNING id,generation,COALESCE(ingress_id,0),state,sales_open,origin`,
		p.command.Event, p.actor, ingress, origin, state, observedOpen, bookingTime, p.registrationRetention.Microseconds(), p.nativeReceivedAt, observed).
		Scan(&result.ID, &result.Generation, &result.Position, &result.State, &result.SalesOpen, &result.Origin)
	return result, core.DatabaseOperationError(err)
}

func (p *PreparedCommand) checkAdmission(ctx context.Context) error {
	if !InitiatesRegistration(p.command) {
		return nil
	}

	result, found, err := p.existingAdmission(ctx)
	if err != nil {
		return err
	}
	if !found {
		return conflict("pass_admission_required")
	}
	if result.State == cancelled {
		return conflict("pass_admission_cancelled")
	}
	return nil
}

func (s *snapshot) persistAdmissions(ctx context.Context, tx pgx.Tx) error {
	for owner := range s.dirty {
		b := s.bookings[owner]
		state, reason := admissionRegistered, ""
		if b.State == cancelled {
			state, reason = cancelled, "registration_cancelled"
		}
		if _, err := tx.Exec(
			ctx,
			`UPDATE core.registration_intents SET state=$3,booking_created_at=$4,terminal_reason=$5,
  closed_through_position=CASE WHEN $3='cancelled' THEN COALESCE((SELECT MAX(id) FROM core.registration_ingress),0) ELSE closed_through_position END
  WHERE event_id=$1 AND owner=$2 AND state<>'cancelled'`,
			s.event.id,
			owner,
			state,
			b.CreatedAt,
			reason,
		); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	return nil
}

// cancelUnfinishedAdmission closes only a captured generation without creating a
// booking row. The cancellation operation and terminal state commit together.
func (p *PreparedCommand) cancelUnfinishedAdmission(ctx context.Context) (bool, error) {
	if p.command.Name != capabilityCancel || p.current.Version != 0 {
		return false, nil
	}
	tag, err := p.tx.Exec(
		ctx,
		`UPDATE core.registration_intents SET state='cancelled',terminal_reason='registration_cancelled',
 closed_through_position=COALESCE((SELECT MAX(id) FROM core.registration_ingress),0)
 WHERE event_id=$1 AND owner=$2 AND state='captured'`,
		p.command.Event,
		p.actor,
	)
	if err != nil || tag.RowsAffected() == 0 {
		return false, core.DatabaseOperationError(err)
	}
	_, err = p.tx.Exec(
		ctx,
		`INSERT INTO core.pass_booking_operations(event_id,actor,key_hash,request_hash) VALUES($1,$2,$3,$4)`,
		p.command.Event,
		p.actor,
		p.keyHash,
		p.requestHash,
	)
	if err == nil {
		p.current.State = cancelled
	}
	return true, core.DatabaseOperationError(err)
}

func (p *PreparedCommand) reconcileAdmission(
	ctx context.Context, result Admission, ref *registrationingress.Reference,
) (Admission, error) {
	// Ordinary operation replay has no new ingress evidence.
	if ref == nil {
		return result, nil
	}
	position, err := p.admissionPosition(ctx, ref)
	if err != nil {
		return Admission{}, err
	}
	if result.State == cancelled || result.Origin != admissionCanonical || position >= result.Position {
		return result, nil
	}
	var retired bool
	if err = p.tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.registration_intents
 WHERE event_id=$1 AND owner=$2 AND state='cancelled' AND closed_through_position >= $3)`,
		p.command.Event, p.actor, position).Scan(&retired); err != nil {
		return Admission{}, core.DatabaseOperationError(err)
	}
	if retired {
		return Admission{}, conflict("pass_admission_cancelled")
	}
	_, err = p.tx.Exec(
		ctx,
		"UPDATE core.registration_intents SET ingress_id=$2,effective_position=CASE WHEN requeue_count=0 AND state='captured' THEN $2 ELSE effective_position END,turn_expires_at=CASE WHEN requeue_count=0 AND state='captured' AND $3::timestamptz IS NOT NULL THEN LEAST(turn_expires_at,$3::timestamptz+$4::bigint*interval '1 microsecond') ELSE turn_expires_at END WHERE id=$1",
		result.ID,
		position,
		p.nativeReceivedAt,
		p.registrationRetention.Microseconds(),
	)
	result.Position = position
	return result, core.DatabaseOperationError(err)
}

// reconcileStaleAdmission retains earlier trusted ordering evidence without
// accepting the stale command as a domain effect or creating another request.
func (p *PreparedCommand) reconcileStaleAdmission(
	ctx context.Context,
	ref *registrationingress.Reference,
) (Admission, error) {
	if ref == nil || p.command.Version > p.current.Version {
		return Admission{}, conflict("pass_booking_stale")
	}
	var result Admission
	err := p.tx.QueryRow(
		ctx,
		`SELECT id,generation,COALESCE(ingress_id,0),state,sales_open,origin
 FROM core.registration_intents WHERE event_id=$1 AND owner=$2 AND state<>'cancelled'`,
		p.command.Event,
		p.actor,
	).Scan(&result.ID, &result.Generation, &result.Position, &result.State, &result.SalesOpen, &result.Origin)
	if errors.Is(err, pgx.ErrNoRows) {
		return Admission{}, conflict("pass_booking_stale")
	}
	if err != nil {
		return Admission{}, core.DatabaseOperationError(err)
	}
	previous := result.Position
	result, err = p.reconcileAdmission(ctx, result, ref)
	if err == nil && result.Position == previous {
		return Admission{}, conflict("pass_booking_stale")
	}
	return result, err
}
