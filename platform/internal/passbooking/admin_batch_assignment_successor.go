package passbooking

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// prepareAssignmentSuccessor advances only the original booking identity through
// canonical earlier receipts of this locked batch. Command hashes remain original.
func (b *RuntimeBatchState) prepareAssignmentSuccessor(
	ctx context.Context,
	tx pgx.Tx,
	index int,
	p *PreparedAssignment,
) error {
	transitions, err := b.AssignmentTransitions(ctx, tx, index)
	if err != nil {
		return err
	}
	p.actorVersion, err = b.assignmentSuccessorVersion(b.actor, p.command.Version, p.records[b.actor], transitions)
	if err != nil {
		return err
	}
	p.targetVersion, err = b.assignmentSuccessorVersion(
		p.command.Target,
		p.command.TargetVersion,
		p.records[p.command.Target],
		transitions,
	)
	return err
}

func (b *RuntimeBatchState) assignmentSuccessorVersion(
	owner string,
	original int64,
	live *Booking,
	transitions []BookingTransition,
) (int64, error) {
	identity, pinned := b.plan.Bookings[owner]
	// Older plans and absent bookings retain strict original-version behavior.
	if !pinned || original <= 0 {
		return original, nil
	}
	if identity.Version != original || live == nil || live.Event != b.plan.Event ||
		live.Owner != owner || live.TelegramID != identity.TelegramID || !live.CreatedAt.Equal(identity.CreatedAt) {
		return original, conflict("pass_booking_stale")
	}
	version := original
	for _, transition := range transitions {
		if transition.Before.Owner == owner && transition.Before.Version == version &&
			transition.Before.CreatedAt.Equal(identity.CreatedAt) {
			version = transition.After.Version
		}
	}
	return version, nil
}
