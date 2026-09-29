package derivedmutation

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (s Service) readTransportOperation(
	ctx context.Context,
	actor string,
	input PassOperationInput,
	summary PassOperationSummary,
) (PassOperationRead, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return PassOperationRead{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var refs []passbooking.ReadAuthority
	switch {
	case input.Export:
		refs, err = passbooking.ExportOperationAuthority(ctx, tx, actor)
	case input.Menu != nil:
		menu := input.Menu
		ref := passbooking.ReadAuthority{Kind: passbooking.ReadOwnerMenu, Event: menu.Event}
		if menu.Historical {
			ref = passbooking.ReadAuthority{Kind: passbooking.ReadOwnedEvent, Event: menu.Event}
		}
		refs = []passbooking.ReadAuthority{ref}
		var valid []bool
		valid, err = passbooking.LockReadAuthorities(ctx, tx, actor, refs)
		if err == nil && (len(valid) != 1 || !valid[0]) {
			err = unavailablePassOperation()
		}
	default:
		err = unavailablePassOperation()
	}
	if err != nil {
		return PassOperationRead{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return PassOperationRead{}, err
	}
	// No delivery receipt is available. These references prove only the current
	// right to see opaque metadata, never historical file contents or delivery.
	return PassOperationRead{Summary: summary, ReadAuthorities: readsource.Registration(refs)}, nil
}
