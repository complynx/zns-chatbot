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
	events := append([]string{}, targets...)
	for _, a := range sourceLeaves(refs) {
		if a.Registration != (passbooking.ReadAuthority{}) {
			events = append(events, a.Registration.Event)
		}
	}
	return passbooking.LockMutationEvents(ctx, tx, events)
}
