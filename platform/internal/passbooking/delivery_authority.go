package passbooking

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

// LockDeliveryExportInTx checks every event included in the rendered file.
func LockDeliveryExportInTx(ctx context.Context, tx pgx.Tx, actor string, events []string) error {
	if !ValidExportEvents(events) {
		return invalid()
	}
	refs := make([]ReadAuthority, len(events))
	for i, event := range events {
		refs[i] = ReadAuthority{Kind: ReadExportPermission, Event: event}
	}
	valid, err := LockReadAuthorities(ctx, tx, actor, refs)
	if err != nil {
		return err
	}
	if slices.Contains(valid, false) {
		return forbidden()
	}
	return nil
}

// LockDeliveryProofInTx keeps the current payment admin/owner and exact attempt
// locked through admission. File bytes remain outside the delivery transaction.
func LockDeliveryProofInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor, event, owner string,
	version int64,
	attempt string,
) error {
	if actor != owner {
		var found string
		err := tx.QueryRow(ctx, `SELECT owner FROM core.pass_payment_admins WHERE event_id=$1 AND owner=$2 FOR SHARE`, event, actor).
			Scan(&found)
		if errors.Is(err, pgx.ErrNoRows) {
			return forbidden()
		}
		if err != nil {
			return err
		}
	}
	var currentVersion int64
	var currentAttempt string
	err := tx.QueryRow(ctx, `SELECT b.version,p.id FROM core.pass_bookings b JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt WHERE b.event_id=$1 AND b.owner=$2 AND b.state IN ('assigned','paid') AND p.proof_id IS NOT NULL FOR SHARE OF b,p`, event, owner).
		Scan(&currentVersion, &currentAttempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	if err != nil {
		return err
	}
	if currentVersion != version || currentAttempt != attempt {
		return conflict("pass_receipt_unavailable")
	}
	return nil
}

// DeliveryExportEvents supplies the bounded event set before the caller locks actors.
func DeliveryExportEvents(ctx context.Context, tx pgx.Tx, actor string) ([]string, error) {
	return exportSnapshotEvents(ctx, tx, actor)
}
