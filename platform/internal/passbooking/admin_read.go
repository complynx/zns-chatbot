package passbooking

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

// AdminTarget exposes assignment guards, not stored legal names or passports.
type AdminTarget struct {
	Booking              Booking             `json:"booking"`
	Name                 string              `json:"name"`
	ActorVersion         int64               `json:"actor_version"`
	ProfileVersion       int64               `json:"profile_version"`
	Role                 passallocation.Role `json:"role"`
	HasLegalName         bool                `json:"has_legal_name"`
	ProfileFrozen        bool                `json:"profile_frozen"`
	CanAssign            bool                `json:"can_assign"`
	CanCreateFromProfile bool                `json:"can_create_from_profile"`
	CurrentTier          int                 `json:"current_tier"`
}

// AdminTarget looks up one exact Telegram identity after global authorization.
// The adapter must ground model-supplied IDs in current user evidence.
func (s Service) AdminTarget(ctx context.Context, actor, eventID string, telegramID int64) (AdminTarget, error) {
	var result AdminTarget
	if telegramID <= 0 {
		return result, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	e, err := readEvent(ctx, tx, eventID)
	if err != nil {
		return result, err
	}
	if _, err = authorize(ctx, tx, actor, commandAdminAssign, eventID); err != nil {
		return result, err
	}
	result.Booking.Event, result.Booking.TelegramID = eventID, telegramID
	err = tx.QueryRow(ctx, `SELECT u.id,u.name,COALESCE(p.version,0),COALESCE(p.role,''),
COALESCE(p.legal_name<>'',false),COALESCE(p.frozen,false)
FROM core.users u LEFT JOIN core.pass_profiles p ON p.owner=u.id WHERE u.telegram_id=$1 AND u.can_book`, telegramID).
		Scan(&result.Booking.Owner, &result.Name, &result.ProfileVersion, &result.Role, &result.HasLegalName, &result.ProfileFrozen)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, forbidden()
	}
	if err != nil {
		return result, core.DatabaseOperationError(err)
	}
	records, err := readBookings(ctx, tx, eventID)
	if err != nil {
		return result, err
	}
	result.ActorVersion = bookingVersion(records[actor])
	if booking := records[result.Booking.Owner]; booking != nil {
		result.Booking = *booking
	}
	if result.Role == "" {
		err = tx.QueryRow(ctx, `SELECT role FROM core.pass_bookings WHERE owner=$1 ORDER BY created_at DESC,event_id LIMIT 1`, result.Booking.Owner).
			Scan(&result.Role)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return result, core.DatabaseOperationError(err)
		}
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return result, core.DatabaseOperationError(err)
	}
	result.CurrentTier = newSnapshot(e, records, now).currentAdminTier()
	result.CanAssign = now.Before(e.finishes) && result.Booking.State != pending
	result.CanCreateFromProfile = result.CanAssign &&
		(result.Booking.Version == 0 || result.Booking.State == cancelled) &&
		(result.Role == passallocation.Leader || result.Role == passallocation.Follower)
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}
