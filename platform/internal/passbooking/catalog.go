package passbooking

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
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
	From      Contact   `json:"from"`
	Version   int64     `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

func (s Service) Events(ctx context.Context, actor string) ([]Event, error) {
	if err := s.requireActor(ctx, actor); err != nil {
		return nil, err
	}
	now, configured, err := registrationingress.Observe(ctx, s.RegistrationClock)
	if err != nil {
		return nil, err
	}
	var observed *time.Time
	if configured {
		observed = &now
	}
	rows, err := s.DB.Query(ctx, `WITH selected AS MATERIALIZED (
 SELECT e.id,e.finishes_at,e.passport_required,e.open_ended,e.display_order,
 (SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id) AS sales_start,
 octet_length(e.id)::bigint+octet_length(e.titles::text)+octet_length(e.short_titles::text)
 +octet_length(e.country_emoji) AS payload_bytes
 FROM core.pass_events e WHERE e.finishes_at>COALESCE($3::timestamptz,clock_timestamp())
 ORDER BY COALESCE((SELECT min(starts_at) FROM core.pass_event_tiers WHERE event_id=e.id),
 '9999-12-31 23:59:59.999999+00'::timestamptz),display_order,e.id LIMIT $1
 ), bounded AS (
 SELECT id,finishes_at,passport_required,open_ended,display_order,sales_start,
 sum(payload_bytes) OVER () > $2 AS oversized FROM selected)
 SELECT CASE WHEN b.oversized THEN '' ELSE b.id END,
 CASE WHEN b.oversized THEN '{}'::jsonb ELSE e.titles END,b.finishes_at,b.passport_required,b.sales_start,
 CASE WHEN b.oversized THEN '{}'::jsonb ELSE e.short_titles END,
 CASE WHEN b.oversized THEN '' ELSE e.country_emoji END,b.open_ended,b.oversized
 FROM bounded b JOIN core.pass_events e ON e.id=b.id
 ORDER BY COALESCE(b.sales_start,'9999-12-31 23:59:59.999999+00'::timestamptz),b.display_order,b.id`,
		maxCatalogEvents+1, core.ReadResourceBytes, observed)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	return collectCatalog(rows)
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
		return nil, core.DatabaseOperationError(err)
	}
	contacts, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Contact])
	return contacts, core.DatabaseOperationError(err)
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
		return InvitationPage{}, core.DatabaseOperationError(err)
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
		if err = rows.Scan(
			&invite.From.Owner, &invite.From.Name, &invite.From.TelegramID, &invite.Version, &invite.CreatedAt,
		); err != nil {
			return InvitationPage{}, core.DatabaseOperationError(err)
		}
		page.Invitations = append(page.Invitations, invite)
		boundary = pageCursor(invite.CreatedAt, invite.From.TelegramID)
	}
	return page, core.DatabaseOperationError(rows.Err())
}

func (s Service) requireActor(ctx context.Context, actor string) error {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, actor).
		Scan(&exists); err != nil {
		return core.DatabaseOperationError(err)
	}
	if !exists {
		return forbidden()
	}
	return nil
}
