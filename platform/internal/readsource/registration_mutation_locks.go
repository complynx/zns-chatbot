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

// LockRegistrationMutationPrelude expands each bounded persisted record for one
// lock pass. Their combined window need not fit one persisted-record budget.
func LockRegistrationMutationPrelude(
	ctx context.Context, tx pgx.Tx, refs []Authority, targets, actors []string,
	additional ...[]Authority,
) error {
	records := append([][]Authority{refs}, additional...)
	for _, record := range records {
		if !Valid(record) {
			return ErrLimit
		}
	}
	var closures [][]Authority
	for _, record := range records {
		expanded, err := proposalClosures(ctx, tx, record)
		if err != nil {
			return err
		}
		closures = append(closures, expanded...)
	}
	expanded := proposalWindowAuthorities(closures)
	if err := lockRegistrationMutationEvents(ctx, tx, expanded, targets); err != nil {
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
