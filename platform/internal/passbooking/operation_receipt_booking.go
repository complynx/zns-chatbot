package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// WitnessOwnerBookingReceipt observes current owner state after canonical invite
// commitment. The caller holds event/actor locks; this never executes a command.
func (s Service) WitnessOwnerBookingReceipt(
	ctx context.Context, tx pgx.Tx, actor string, witness OperationWitness,
) (OperationReceipt, Booking, error) {
	if !witness.Valid(actor) || witness.Family != OperationCommand ||
		witness.Action != commandInvite || witness.Target != "" || witness.QueueInvitation {
		return OperationReceipt{}, Booking{}, forbidden()
	}
	receipt, err := s.WitnessOperationReceipt(ctx, tx, actor, witness, nil)
	if err != nil {
		return OperationReceipt{}, Booking{}, err
	}
	if receipt.Status != operationCommitted {
		return OperationReceipt{}, Booking{}, forbidden()
	}
	rows, err := tx.Query(ctx, `SELECT `+bookingColumns+`
 FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner
 WHERE b.event_id=$1 AND b.owner=$2 FOR SHARE OF b`, witness.Event, actor)
	if err != nil {
		return OperationReceipt{}, Booking{}, core.DatabaseOperationContextError(ctx, err)
	}
	booking, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Booking])
	if errors.Is(err, pgx.ErrNoRows) {
		return OperationReceipt{}, Booking{}, forbidden()
	}
	if err != nil {
		return OperationReceipt{}, Booking{}, core.DatabaseOperationContextError(ctx, err)
	}
	receipt.ReadAuthorities = append(receipt.ReadAuthorities, ReadAuthority{Kind: ReadOwnerBooking,
		Event: booking.Event, Owner: booking.Owner, Version: booking.Version, CreatedAt: booking.CreatedAt})
	receipt.ReadAuthorities, err = checkedOperationAuthorities(ctx, tx, actor, receipt.ReadAuthorities)
	if err != nil {
		return OperationReceipt{}, Booking{}, err
	}
	return receipt, booking, nil
}
