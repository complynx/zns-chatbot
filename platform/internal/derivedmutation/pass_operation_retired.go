package derivedmutation

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) readRetiredPassOperation(
	ctx context.Context,
	actor string,
	input PassOperationInput,
) (PassOperationRead, error) {
	if input.Witness == nil || !input.Witness.Valid(actor) || !operationWitnessMatches(input) {
		return PassOperationRead{}, unavailablePassOperation()
	}
	w := *input.Witness
	actors := []string{actor}
	if w.Target != "" {
		actors = append(actors, w.Target)
	}
	var batch *passbooking.RuntimeBatchState
	if w.Family == passbooking.OperationBatch {
		var err error
		batch, err = s.Registration.WitnessBatch(ctx, actor, w)
		if errors.Is(err, pgx.ErrNoRows) {
			return PassOperationRead{}, unavailablePassOperation()
		}
		if err != nil {
			return PassOperationRead{}, err
		}
		actors = batch.Actors()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return PassOperationRead{}, core.DatabaseOperationContextError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockBatchPrelude(ctx, tx, w.Event, actors, nil); err != nil {
		return PassOperationRead{}, err
	}
	var receipt passbooking.OperationReceipt
	var booking *passbooking.Booking
	if input.ObserveOwnerBooking {
		var current passbooking.Booking
		receipt, current, err = s.Registration.WitnessOwnerBookingReceipt(ctx, tx, actor, w)
		if err == nil {
			booking = &current
		}
	} else {
		receipt, err = s.Registration.WitnessOperationReceipt(ctx, tx, actor, w, batch)
	}
	if err != nil {
		return PassOperationRead{}, err
	}
	summary := PassOperationSummary{Continuation: "unavailable"}
	applyReceiptStatus(&summary, receipt)
	if err = tx.Commit(ctx); err != nil {
		return PassOperationRead{}, core.DatabaseOperationContextError(ctx, err)
	}
	return PassOperationRead{Summary: summary, CurrentBooking: booking,
		ReadAuthorities: readsource.Registration(receipt.ReadAuthorities)}, nil
}

func operationWitnessMatches(input PassOperationInput) bool {
	w := input.Witness
	switch w.Family {
	case passbooking.OperationCommand:
		c := input.Command
		return c != nil && c.Event == w.Event && c.Key == w.Key && c.Name == w.Action && c.Target == w.Target &&
			c.Version == w.Version && c.TargetVersion == w.TargetVersion && c.InviteTelegramID == w.InviteTelegramID &&
			c.PaymentAdmin == w.PaymentAdmin && c.QueueInvitation == w.QueueInvitation
	case passbooking.OperationAssignment:
		c := input.Assignment
		return c != nil && c.Event == w.Event && c.Key == w.Key && c.Target == w.Target && c.Version == w.Version &&
			c.TargetVersion == w.TargetVersion
	case passbooking.OperationBatch:
		c := input.Batch
		return c != nil && c.Event == w.Event && c.Key == w.Key && c.Action == w.Action &&
			slices.Equal(c.Recipients, w.Recipients)
	default:
		return false
	}
}
