package derivedmutation

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// lockRegistrationPrelude orders the union at UPDATE strength, including source
// events that differ from the target, before any actor or source grant lock.
func lockRegistrationPrelude(
	ctx context.Context,
	tx pgx.Tx,
	actor, target, event string,
	source readsource.Derivation,
) error {
	if err := readsource.LockRegistrationMutationEvents(ctx, tx, source.Authorities, []string{event}); err != nil {
		return err
	}
	actors := []string{actor}
	if target != "" {
		actors = append(actors, target)
	}
	return readsource.LockActors(ctx, tx, actors, source.Authorities)
}

func (s Service) ExecutePassBooking(
	ctx context.Context,
	actor string,
	command passbooking.Command,
	source readsource.Derivation,
) (passbooking.Booking, error) {
	if !source.Valid() {
		return passbooking.Booking{}, invalidSource()
	}
	source = source.Clone()
	if _, err := s.CapturePassAdmission(
		ctx,
		actor,
		passbooking.AdmissionRequest{Command: command},
		source,
	); err != nil {
		return passbooking.Booking{}, err
	}
	if err := s.Registration.ResolveRegistrationIntake(ctx, command.Event); err != nil {
		return passbooking.Booking{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return passbooking.Booking{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockRegistrationPrelude(ctx, tx, actor, command.Target, command.Event, source); err != nil {
		return passbooking.Booking{}, err
	}
	prepared, err := s.Registration.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return passbooking.Booking{}, err
	}
	return commitPrepared(ctx, tx, actor, source, prepared)
}

func (s Service) AssignPass(
	ctx context.Context,
	actor string,
	command passbooking.AdminAssignment,
	source readsource.Derivation,
) (passbooking.AdminAssignmentResult, error) {
	if err := s.Registration.ResolveRegistrationIntake(ctx, command.Event); err != nil {
		return passbooking.AdminAssignmentResult{}, err
	}
	if !source.Valid() {
		return passbooking.AdminAssignmentResult{}, invalidSource()
	}
	source = source.Clone()
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return passbooking.AdminAssignmentResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockRegistrationPrelude(ctx, tx, actor, command.Target, command.Event, source); err != nil {
		return passbooking.AdminAssignmentResult{}, err
	}
	prepared, err := s.Registration.PrepareAssignmentInTx(ctx, tx, actor, command)
	if err != nil {
		return passbooking.AdminAssignmentResult{}, err
	}
	return commitPrepared(ctx, tx, actor, source, prepared)
}
