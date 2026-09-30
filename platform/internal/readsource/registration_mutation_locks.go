package readsource

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// LockRegistrationMutationEvents includes target events in the same sorted lock
// set as all source leaves, including flat inherited origin groups.
func LockRegistrationMutationEvents(ctx context.Context, tx pgx.Tx, refs []Authority, targets []string) error {
	if !Valid(refs) {
		return errors.New("invalid source authority")
	}
	return lockRegistrationMutationEvents(ctx, tx, refs, targets)
}

// LockRegistrationMutationPrelude expands individually bounded records for one
// lock pass. The input budget still bounds the number of opaque records; their
// combined validation window need not fit a single persisted-record budget.
func LockRegistrationMutationPrelude(
	ctx context.Context, tx pgx.Tx, refs []Authority, targets, actors []string,
) error {
	if !Valid(refs) {
		return ErrLimit
	}
	closures, err := proposalClosures(ctx, tx, refs)
	if err != nil {
		return err
	}
	expanded := proposalWindowAuthorities(closures)
	if err = lockRegistrationMutationEvents(ctx, tx, expanded, targets); err != nil {
		return err
	}
	return lockActors(ctx, tx, actors, expanded, true)
}

func lockRegistrationMutationEvents(ctx context.Context, tx pgx.Tx, refs []Authority, targets []string) error {
	events := append([]string{}, targets...)
	for _, a := range sourceLeaves(refs) {
		if a.Registration != (passbooking.ReadAuthority{}) {
			events = append(events, a.Registration.Event)
		}
	}
	return passbooking.LockMutationEvents(ctx, tx, events)
}
