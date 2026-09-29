package delivery

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Registration describes an already inserted owner's payload-free queue binding.
// Owner rows and source authority must be locked before the final batch phase.
type Registration struct {
	Reference   Reference
	Destination Destination
	Class       Class
}

// RegisterBatch is the final registration phase of a caller-owned transaction.
// It validates all requests before writing, sorts lanes once, and preserves the
// caller's order within each lane. An empty batch does not require a bot binding.
// Roll back on any error, and do not acquire new owner locks after this call.
func RegisterBatch(ctx context.Context, tx pgx.Tx, botID int64, requests []Registration) error {
	if len(requests) == 0 {
		return nil
	}
	if botID <= 0 {
		return ErrQueueReference
	}
	for _, request := range requests {
		if !request.Reference.valid() || !validDestination(request.Destination) ||
			(request.Class != Interactive && request.Class != Background) {
			return ErrQueueReference
		}
	}
	ordered := append([]Registration(nil), requests...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Destination.Chat < ordered[j].Destination.Chat })
	for _, request := range ordered {
		if _, err := Register(ctx, tx, botID, request.Reference, request.Destination, request.Class); err != nil {
			return err
		}
	}
	return nil
}
