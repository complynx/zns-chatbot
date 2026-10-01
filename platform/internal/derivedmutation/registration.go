package derivedmutation

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// lockRegistrationPrelude orders the union at UPDATE strength, including source
// events hidden by opaque memory/proposal provenance, before actor or grant locks.
func lockRegistrationPrelude(
	ctx context.Context,
	tx pgx.Tx,
	actor, target, event string,
	source readsource.Derivation,
) error {
	actors := []string{actor}
	if target != "" {
		actors = append(actors, target)
	}
	return readsource.LockRegistrationMutationPrelude(ctx, tx, source.Authorities, []string{event}, actors)
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
		return passbooking.Booking{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	registration, clockAttempt := s.Registration.WithClockAttempt()
	if err = lockRegistrationPrelude(ctx, tx, actor, command.Target, command.Event, source); err != nil {
		return passbooking.Booking{}, err
	}
	prepared, err := registration.PrepareInTx(ctx, tx, actor, command)
	if err != nil {
		return passbooking.Booking{}, err
	}
	return commitRegistrationPrepared(ctx, tx, actor, source, prepared, clockAttempt)
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
		return passbooking.AdminAssignmentResult{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	registration, clockAttempt := s.Registration.WithClockAttempt()
	if err = lockRegistrationPrelude(ctx, tx, actor, command.Target, command.Event, source); err != nil {
		return passbooking.AdminAssignmentResult{}, err
	}
	prepared, err := registration.PrepareAssignmentInTx(ctx, tx, actor, command)
	if err != nil {
		return passbooking.AdminAssignmentResult{}, err
	}
	return commitRegistrationPrepared(ctx, tx, actor, source, prepared, clockAttempt)
}

// Registration effects retain their domain time through the outer source commit.
// Other domains keep commitPrepared's existing contract.
func commitRegistrationPrepared[T any](
	ctx context.Context,
	tx pgx.Tx,
	actor string,
	source readsource.Derivation,
	prepared preparedMutation[T],
	clockAttempt *passbooking.RegistrationClockAttempt,
) (T, error) {
	if result, found := prepared.Replay(); found {
		return result, nil
	}
	var zero T
	if err := lockSource(ctx, tx, actor, source); err != nil {
		return zero, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return zero, clockAttempt.DecisionError(ctx, err)
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return zero, err
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}
