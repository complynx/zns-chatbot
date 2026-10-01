package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
)

// PreparedAdmission can capture an intake but cannot allocate a booking. The
// caller commits it before opening the event-serialized effect transaction.
type PreparedAdmission struct{ command *PreparedCommand }

func (s Service) PrepareAdmissionInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	c Command,
) (*PreparedAdmission, error) {
	if err := validate(c); err != nil {
		return nil, err
	}
	if !InitiatesRegistration(c) {
		return nil, invalid()
	}
	// The existing actor row serializes concurrent intakes for this owner without
	// holding the global ingress allocator while updating an existing intent.
	var owner string
	err := tx.QueryRow(ctx, `SELECT id FROM core.users WHERE id=$1 FOR NO KEY UPDATE`, actor).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, forbidden()
	}
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	e, err := readEventLocked(ctx, tx, c.Event, false)
	if err != nil {
		return nil, err
	}
	prepared, err := s.prepareCommand(ctx, tx, actor, c, e)
	if err != nil {
		return nil, err
	}
	return &PreparedAdmission{command: prepared}, nil
}

func (p *PreparedAdmission) Replayed() bool { return p.command.found }

func (p *PreparedAdmission) Capture(ctx context.Context, ref *registrationingress.Reference) (Admission, error) {
	if ref != nil && !ref.Valid() {
		return Admission{}, invalid()
	}
	return p.command.captureAdmission(ctx, ref)
}

// CaptureNative requires the exact current version. Ordinary historical aliases
// use Capture; a newly classified stale button must not create or reprioritize it.
func (p *PreparedAdmission) CaptureNative(ctx context.Context, ref *registrationingress.Reference) (Admission, error) {
	if p.command.current.Version != p.command.command.Version && !p.command.found {
		return Admission{}, conflict("pass_booking_stale")
	}
	return p.Capture(ctx, ref)
}

// RefreshAdmissionTurnsInTx holds the event writer fence before rotating a newly
// materialized native turn whose original reception deadline already elapsed.
func (s Service) RefreshAdmissionTurnsInTx(ctx context.Context, tx pgx.Tx, eventID string) error {
	if _, err := readEvent(ctx, tx, eventID); err != nil {
		return err
	}
	now, err := registrationTime(ctx, tx, s.RegistrationClock)
	if err != nil {
		return err
	}
	return refreshRegistrationTurns(ctx, tx, eventID, s.registrationRetention(), now)
}
