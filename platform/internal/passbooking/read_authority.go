package passbooking

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

// LockReadAuthorities follows mutation lock order: sorted events, actor, grants,
// then bookings. The caller retains the transaction until derived data is used.
// Input can combine multiple individually bounded persisted provenance records.
func LockReadAuthorities(ctx context.Context, tx pgx.Tx, actor string, authorities []ReadAuthority) ([]bool, error) {
	locked, err := LockReadAuthorityEvents(ctx, tx, authorities)
	if err != nil {
		return nil, err
	}
	queries := dbgen.New(tx)
	telegramID, err := queries.LockReadActor(ctx, actor)
	if err != nil {
		return nil, err
	}
	valid := make([]bool, len(authorities))
	for i, a := range authorities {
		if !slices.Contains(locked, a.Event) {
			continue
		}
		valid[i], err = lockReadAuthority(ctx, tx, actor, telegramID, a)
		if err != nil {
			return nil, err
		}
	}
	return valid, nil
}

// LockReadAuthorityEvents acquires source event locks before a mutation locks
// its target event and actors. It does not authorize use of any source.
func LockReadAuthorityEvents(ctx context.Context, tx pgx.Tx, authorities []ReadAuthority) ([]string, error) {
	events := make([]string, 0, len(authorities))
	for _, a := range authorities {
		if !a.valid() {
			return nil, invalid()
		}
		events = append(events, a.Event)
	}
	slices.Sort(events)
	events = slices.Compact(events)
	return dbgen.New(tx).LockReadEvents(ctx, events)
}

func lockReadAuthority(ctx context.Context, tx pgx.Tx, actor string, telegramID int64, a ReadAuthority) (bool, error) {
	switch a.Kind {
	case ReadOwnerMenu:
		// The enclosing read locked the current actor and event. Get requires
		// no booking permission and does not hide finished events.
		return true, nil
	case ReadExportPermission:
		return lockExportPermission(ctx, tx, actor, a.Event)
	case ReadOperationTarget:
		return lockReadPrivilege(ctx, tx, actor, a)
	case ReadPaymentRole:
		return lockPaymentReadRole(ctx, tx, actor, a.Event)
	case ReadPrivileged, ReadCapability:
		allowed, err := lockReadPrivilege(ctx, tx, actor, a)
		if err != nil || !allowed || a.Owner == "" {
			return allowed, err
		}
	case ReadOwnerBooking:
		if a.Owner != actor {
			return false, nil
		}
	case ReadOwnedEvent:
		a.Owner = actor
	case ReadInvitation:
	}
	b, err := dbgen.New(tx).LockReadBooking(ctx, dbgen.LockReadBookingParams{EventID: a.Event, Owner: a.Owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if a.Kind == ReadOwnedEvent {
		return true, nil
	}
	if b.Version != a.Version || !b.CreatedAt.Time.Equal(a.CreatedAt) {
		return false, nil
	}
	if a.PaymentAttempt != "" {
		return lockReadPaymentQueue(ctx, tx, a, b)
	}
	return a.Kind != ReadInvitation || b.State == pending && b.InvitationTarget == telegramID, nil
}

// A pending-review row derives authority from the current paid participant and
// its pending attempt. Historical payment reads use their own separate scope.
func lockReadPaymentQueue(ctx context.Context, tx pgx.Tx, a ReadAuthority, b dbgen.LockReadBookingRow) (bool, error) {
	if b.State != paid || b.PaymentAttempt != a.PaymentAttempt {
		return false, nil
	}
	eligible, err := dbgen.New(tx).
		LockReadPaymentAttempt(ctx, dbgen.LockReadPaymentAttemptParams{EventID: a.Event, ID: a.PaymentAttempt})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return eligible, nil
}

func lockReadPrivilege(ctx context.Context, tx pgx.Tx, actor string, a ReadAuthority) (bool, error) {
	_, err := authorize(ctx, tx, actor, a.Action, a.Event)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status == http.StatusForbidden {
		return false, nil
	}
	if err != nil || a.TargetTelegramID == 0 {
		return err == nil, err
	}
	return lockReadTarget(ctx, tx, a)
}

func lockReadTarget(ctx context.Context, tx pgx.Tx, a ReadAuthority) (bool, error) {
	queries := dbgen.New(tx)
	target, err := queries.LockReadTarget(ctx, a.TargetTelegramID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if a.Owner != "" && a.Owner != target.ID {
		return false, nil
	}
	if a.Action == commandAdminAssign {
		return target.CanBook, nil
	}
	_, err = queries.LockReadBooking(ctx, dbgen.LockReadBookingParams{EventID: a.Event, Owner: target.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
