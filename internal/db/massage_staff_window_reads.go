package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/massage"
)

const massageStaffWindowPageLimit = 20

// MassageStaffWindowCursor identifies one window version, not a grant or snapshot.
type MassageStaffWindowCursor struct {
	EventID  uuid.UUID `json:"event_id"`
	WindowID string    `json:"window_id"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
}

type MassageStaffWindowsQuery struct {
	EventID uuid.UUID                 `json:"event_id"`
	After   *MassageStaffWindowCursor `json:"after,omitempty"`
}

type MassageStaffWindow struct {
	ID       string                `json:"id"`
	Name     massage.ScheduleLabel `json:"name"`
	StartsAt time.Time             `json:"starts_at"`
	EndsAt   time.Time             `json:"ends_at"`
}

type MassageStaffWindowsPage struct {
	EventID  uuid.UUID                 `json:"event_id"`
	Timezone string                    `json:"timezone"`
	Windows  []MassageStaffWindow      `json:"windows"`
	Next     *MassageStaffWindowCursor `json:"next,omitempty"`
}

// MassageStaffWindows discovers current and retained booked windows in pages of
// at most 20. Historical ranges survive schedule and event date edits. Every page
// requires current private specialist access; no participant pass is required.
func (s *Store) MassageStaffWindows(ctx context.Context, scope ProfileScope,
	args MassageStaffWindowsQuery,
) (MassageStaffWindowsPage, error) {
	return s.staffMassageWindows(ctx, scope, nil, args)
}

func (s *Store) MiniAppMassageStaffWindows(ctx context.Context, deployment EventScope,
	session MiniAppMassageSession, args MassageStaffWindowsQuery,
) (MassageStaffWindowsPage, error) {
	return s.staffMassageWindows(ctx,
		ProfileScope{BotID: deployment.BotID, Organization: deployment.Organization}, &session, args)
}

func (s *Store) staffMassageWindows(ctx context.Context, scope ProfileScope,
	session *MiniAppMassageSession, args MassageStaffWindowsQuery,
) (MassageStaffWindowsPage, error) {
	result := MassageStaffWindowsPage{Windows: []MassageStaffWindow{}}
	if args.EventID == uuid.Nil {
		return result, massage.ErrInvalid
	}
	params := sqlc.ListMassageStaffWindowsParams{EventID: args.EventID,
		AfterStartsAt: pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
		AfterEndsAt:   pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true}}
	if c := args.After; c != nil {
		if c.EventID != args.EventID || !massageSpecialistID.MatchString(c.WindowID) ||
			c.StartsAt.IsZero() || c.EndsAt.IsZero() || c.StartsAt.Year() < 1 || c.EndsAt.Year() > 9999 ||
			!c.StartsAt.Before(c.EndsAt) {
			return result, massage.ErrInvalid
		}
		params.HasAfter = true
		params.AfterWindowID = c.WindowID
		params.AfterStartsAt.Time, params.AfterEndsAt.Time = c.StartsAt, c.EndsAt
	}
	r, err := s.massageStaffReadTx(ctx, scope, session, args.EventID)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, r.tx)
	result.EventID, result.Timezone = r.event.ID, r.event.Timezone
	rows, err := r.q.ListMassageStaffWindows(ctx, params)
	if err != nil {
		return result, err
	}
	for _, row := range rows[:min(len(rows), massageStaffWindowPageLimit)] {
		item := MassageStaffWindow{ID: row.WindowID, StartsAt: row.StartsAt.Time.UTC(), EndsAt: row.EndsAt.Time.UTC()}
		if err = json.Unmarshal(row.Name, &item.Name); err != nil {
			return result, err
		}
		result.Windows = append(result.Windows, item)
	}
	if len(rows) > massageStaffWindowPageLimit {
		last := result.Windows[len(result.Windows)-1]
		result.Next = &MassageStaffWindowCursor{
			EventID:  args.EventID,
			WindowID: last.ID,
			StartsAt: last.StartsAt,
			EndsAt:   last.EndsAt,
		}
	}
	return result, nil
}
