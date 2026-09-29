package derivedmutation

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// lockBatchSource keeps original evidence immutable. Only this batch's canonical
// prior receipts may advance a supported booking leaf; normal checks then fence
// the live successor, all grants, causal origins and unrelated source evidence.
func lockBatchSource(
	ctx context.Context, tx pgx.Tx, actor string, batch *passbooking.RuntimeBatchState,
	index int, original readsource.Derivation,
) error {
	transitions, err := batch.AssignmentTransitions(ctx, tx, index)
	if err != nil {
		return err
	}
	effective := original.Clone()
	for i := range effective.Authorities {
		ref := &effective.Authorities[i]
		if ref.Causal == nil {
			advanceBatchLeaf(&ref.Registration, transitions)
			continue
		}
		// Foreign or published origins do not inherit this actor's own effects.
		if ref.Causal.Actor != actor || ref.Causal.Published {
			continue
		}
		for j := range ref.Causal.Authorities {
			advanceBatchLeaf(&ref.Causal.Authorities[j].Registration, transitions)
		}
	}
	return lockSource(ctx, tx, actor, effective)
}

func advanceBatchLeaf(ref *passbooking.ReadAuthority, transitions []passbooking.BookingTransition) {
	if ref.Kind != passbooking.ReadOwnerBooking &&
		(ref.Kind != passbooking.ReadPrivileged || ref.Action != "admin_assign") {
		return
	}
	for _, transition := range transitions {
		before := transition.Before
		if ref.Event == before.Event && ref.Owner == before.Owner && ref.Version == before.Version &&
			ref.CreatedAt.Equal(before.CreatedAt) {
			ref.Version = transition.After.Version
		}
	}
}
