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
	if err != nil {
		return err
	}
	// Reassigning a current pair can unlink its partner as well as its target.
	target := p.records[p.command.Target]
	if target == nil || target.Partner == "" {
		return nil
	}
	partner := p.records[target.Partner]
	if partner == nil || partner.Partner != target.Owner {
		return nil
	}
	identity, pinned := b.plan.Bookings[partner.Owner]
	if !pinned {
		return nil
	}
	version, err := b.assignmentSuccessorVersion(partner.Owner, identity.Version, partner, transitions)
	if err != nil {
		return err
	}
	if partner.Version != version {
		return conflict("pass_booking_stale")
	}
	return nil
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
