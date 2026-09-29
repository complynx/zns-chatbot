package passbooking

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type Event struct {
	ID               string            `json:"id"`
	Titles           map[string]string `json:"titles"`
	FinishesAt       time.Time         `json:"finishes_at"`
	PassportRequired bool              `json:"passport_required"`
	SalesStart       *time.Time        `json:"sales_start,omitempty"`
	ShortTitles      map[string]string `json:"short_titles"`
	CountryEmoji     string            `json:"country_emoji"`
	OpenEnded        bool              `json:"open_ended"`
}

type Contact struct {
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	TelegramID int64  `json:"telegram_id"`
}

type Invitation struct {
	From    Contact `json:"from"`
	Version int64   `json:"version"`
}

func (s Service) Events(ctx context.Context, actor string) ([]Event, error) {
	if err := s.requireActor(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT e.id,e.titles,e.finishes_at,e.passport_required,
 (SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id),e.short_titles,e.country_emoji,e.open_ended
 FROM core.pass_events e WHERE e.finishes_at>clock_timestamp()
 ORDER BY COALESCE((SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id),'9999-12-31 23:59:59.999999+00'::timestamptz),display_order,e.id LIMIT 101`)
	if err != nil {
		return nil, err
	}
	events, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Event])
	if err != nil {
		return nil, err
	}
	const maxEvents = 100
	if len(events) > maxEvents {
		return nil, conflict("pass_event_limit")
	}
	return events, nil
}

func (s Service) PaymentAdmins(ctx context.Context, actor, eventID string) ([]Contact, error) {
	if err := s.requireActor(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT u.id,u.name,u.telegram_id FROM core.users u
 LEFT JOIN core.pass_payment_admins a ON a.owner=u.id AND a.event_id=$1
 WHERE (a.owner IS NOT NULL AND NOT a.hidden) OR
 (EXISTS(SELECT 1 FROM core.pass_bookings b WHERE b.event_id=$1 AND b.owner=$2 AND b.payment_admin=u.id)
 AND (a.owner IS NOT NULL OR EXISTS(SELECT 1 FROM core.pass_booking_admins g WHERE g.owner=u.id)))
 ORDER BY u.id`, eventID, actor)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Contact])
}

func (s Service) Invitations(ctx context.Context, actor, eventID, after string) (InvitationPage, error) {
	if err := s.requireActor(ctx, actor); err != nil {
		return InvitationPage{}, err
	}
	at, telegramID, err := parsePageCursor(after)
	if err != nil {
		return InvitationPage{}, err
	}
	rows, err := s.DB.Query(ctx, `SELECT b.owner,u.name,u.telegram_id,b.version,b.created_at
 FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner
 JOIN core.users recipient ON recipient.telegram_id=b.invitation_target
 WHERE b.event_id=$1 AND recipient.id=$2 AND b.state='waiting-for-couple'
 AND ($3='' OR (b.created_at,u.telegram_id)>($4,$5))
 ORDER BY b.created_at,u.telegram_id LIMIT $6`, eventID, actor, after, at, telegramID, pageSize+1)
	if err != nil {
		return InvitationPage{}, err
	}
	defer rows.Close()
	page := InvitationPage{Invitations: []Invitation{}}
	var boundary string
	for rows.Next() {
		if len(page.Invitations) == pageSize {
			page.Next = boundary
			break
		}
		var invite Invitation
		var created time.Time
		if err = rows.Scan(
			&invite.From.Owner, &invite.From.Name, &invite.From.TelegramID, &invite.Version, &created,
		); err != nil {
			return InvitationPage{}, err
		}
		page.Invitations = append(page.Invitations, invite)
		boundary = pageCursor(created, invite.From.TelegramID)
	}
	return page, rows.Err()
}

func (s Service) requireActor(ctx context.Context, actor string) error {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, actor).
		Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return forbidden()
	}
	return nil
}
