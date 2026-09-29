package passbooking

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

const DefaultRegistrationRetention = 10 * time.Minute

func (s Service) registrationRetention() time.Duration {
	if s.RegistrationRetention > 0 {
		return s.RegistrationRetention
	}
	return DefaultRegistrationRetention
}

// refreshRegistrationTurns runs under the event lock. It preserves the admission
// identity and draft while moving each expired unfinished turn to the tail once.
func refreshRegistrationTurns(
	ctx context.Context,
	tx pgx.Tx,
	eventID string,
	retention time.Duration,
	now time.Time,
) error {
	q := dbgen.New(tx)
	if err := q.InitializeRegistrationTurns(
		ctx,
		dbgen.InitializeRegistrationTurnsParams{EventID: eventID, RetentionMicroseconds: retention.Microseconds()},
	); err != nil {
		return err
	}
	ids, err := q.ExpiredRegistrationTurns(
		ctx,
		dbgen.ExpiredRegistrationTurnsParams{EventID: eventID, ObservedAt: pgtype.Timestamptz{Time: now, Valid: true}},
	)
	if err != nil || len(ids) == 0 {
		return err
	}
	// Match ingress allocation's short lock so future intake ranks after rotation.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(782619)"); err != nil {
		return err
	}
	for _, id := range ids {
		if err = q.RotateRegistrationTurn(
			ctx,
			dbgen.RotateRegistrationTurnParams{
				ID:         id,
				Deadline:   pgtype.Timestamptz{Time: now.Add(retention), Valid: true},
				ObservedAt: pgtype.Timestamptz{Time: now, Valid: true},
			},
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *snapshot) loadRegistrationRanks(ctx context.Context, tx pgx.Tx) error {
	rows, err := dbgen.New(tx).RegistrationRanks(ctx, s.event.id)
	if err != nil {
		return err
	}
	s.registrationRanks = make(map[string]int64, len(rows))
	s.unfinishedRegistrations = make(map[string]bool)
	for _, row := range rows {
		s.registrationRanks[row.Owner] = row.Position
		if row.State == "captured" {
			s.unfinishedRegistrations[row.Owner] = true
		}
	}
	pending, err := dbgen.New(tx).PendingNativeRegistrationRanks(ctx, s.event.id)
	if err != nil {
		return err
	}
	for _, row := range pending {
		if previous := s.registrationRanks[row.Owner]; previous == 0 || row.Position < previous {
			s.registrationRanks[row.Owner] = row.Position
		}
		s.unfinishedRegistrations[row.Owner] = true
	}
	return nil
}

func (s *snapshot) blockedByRegistration(b *Booking) bool {
	position := s.registrationPosition(b)
	if position == 0 {
		return false
	}
	for owner := range s.unfinishedRegistrations {
		// The current transaction may have completed this draft after ranks loaded.
		if current := s.bookings[owner]; s.dirty[owner] && current != nil && current.State != cancelled {
			continue
		}
		if s.registrationRanks[owner] < position {
			return true
		}
	}
	return false
}

// An accepted partner shares the initiating pair's rank when it has no separate
// admission. The imported fallback stays labelled by an absent canonical rank.
func (s *snapshot) registrationPosition(b *Booking) int64 {
	if position := s.registrationRanks[b.Owner]; position > 0 {
		return position
	}
	return s.registrationRanks[b.Partner]
}
