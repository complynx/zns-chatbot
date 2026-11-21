package db

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// PassTicketInput contains only public event selection data from a private Telegram ingress.
type PassTicketInput struct {
	Organization, Event, Callback, Role string
	PairMode                            bool
	PromptID                            uuid.UUID
	Choice                              int
	MessageID                           int64
}

// PassSlugBinding pins a public command argument before the inbox and its effects commit.
type PassSlugBinding struct {
	Organization, Slug string
	Bind               func(*IntakeRequest, uuid.UUID) error
}

func (b *PassSlugBinding) resolve(ctx context.Context, tx pgx.Tx, r *IntakeRequest) error {
	if b == nil {
		return nil
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
 SELECT id FROM shuttle_events WHERE bot_id=$1 AND organization_id=$2 AND slug=$3 FOR UPDATE
`, r.BotID, b.Organization, b.Slug).Scan(&id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return b.Bind(r, id)
}

type passIntakeSelection struct {
	id        uuid.UUID
	pair      bool
	role      string
	version   *int64
	expiresAt *time.Time
	event     events.Event
}

// Lock the selected event before taking the intake user lock. The ordering is
// shared with assignment. A forged or stale callback cannot change priority.
func passIntakeEvent(ctx context.Context, tx pgx.Tx, r IntakeRequest) (*passIntakeSelection, error) {
	ticket := r.PassTicket
	if ticket == nil || r.ChatID != r.TelegramUserID {
		return nil, nil //nolint:nilnil // The update does not select an active promo.
	}
	selected := passIntakeSelection{pair: ticket.PairMode, role: ticket.Role}
	var err error
	if ticket.PromptID != uuid.Nil {
		selected, err = passPromptEvent(ctx, tx, r)
	} else if parsed, e := uuid.Parse(ticket.Event); e == nil && parsed.String() == ticket.Event {
		selected.id = parsed
	} else {
		return nil, registrations.ErrInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // The update does not select an active promo.
	}
	if err != nil {
		return nil, err
	}
	scope := EventScope{BotID: r.BotID, Organization: ticket.Organization}
	event, err := registrationEventForChange(ctx, sqlc.New(tx), scope, selected.id)
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, registrations.ErrUnavailable) {
		return nil, nil //nolint:nilnil // The update does not select an active promo.
	}
	if err != nil {
		return nil, err
	}
	if !selected.pair && (!event.Document.Registration.AutoAssign || event.Document.Registration.Pricing == nil) {
		return nil, nil //nolint:nilnil // The update does not select an active promo.
	}
	if selected.version != nil && *selected.version != event.Version {
		return nil, nil //nolint:nilnil // The event changed after this pair choice was offered.
	}
	selected.event = event
	return &selected, nil
}

const maxReceiptRetries = 5

func passPromptEvent(ctx context.Context, tx pgx.Tx, r IntakeRequest) (passIntakeSelection, error) {
	ticket := r.PassTicket
	for attempt := 0; ; attempt++ {
		var selected passIntakeSelection
		var delivered bool
		err := tx.QueryRow(ctx, `
 SELECT (CASE WHEN choice->>'action'='event' THEN choice->>'id' ELSE presented->>'event_id' END)::uuid,
   choice->>'action'='mode' OR COALESCE(input->>'mode'='pair',false), COALESCE(input->>'role',''),
   (presented->>'event_version')::bigint, (presented->>'expires_at')::timestamptz, o.state='delivered'
 FROM shuttle_tasks p
 JOIN shuttle_users u ON u.id=p.user_id JOIN shuttle_conversations c ON c.id=p.conversation_id
 JOIN shuttle_outbox o ON o.task_id=p.id
 CROSS JOIN LATERAL (
   SELECT p.snapshot#>'{pass_entry,choices}' -> $6::int AS choice,
     p.snapshot#>'{pass_entry,input}' AS input, p.snapshot#>'{pass_entry,event}' AS presented
 ) v
 WHERE p.id=$1 AND u.bot_id=$2 AND u.tg_user_id=$3 AND c.chat_id=$3 AND NOT u.revoked
 AND p.route='structured' AND p.state='completed' AND p.request->>'structured_command'='/passes'
 AND p.created_at+interval '2 hours'>clock_timestamp()
   AND (o.state IN ('pending','retryable','send_inflight') OR (o.state='delivered'
   AND o.receipt->>'message_id'=$4))
 AND o.payload#>'{reply_markup,inline_keyboard}' @>
   jsonb_build_array(jsonb_build_array(jsonb_build_object('callback_data',$5::text)))
 AND COALESCE(input->>'code','')=''
 AND (choice->>'action'='event' OR (choice->>'action'='mode' AND choice->>'value'='pair'
   AND presented->>'event_id' IS NOT NULL AND presented->>'event_version' IS NOT NULL
   AND presented->>'expires_at' IS NOT NULL))
 LIMIT 1
`, ticket.PromptID, r.BotID, r.TelegramUserID,
			strconv.FormatInt(ticket.MessageID, 10), ticket.Callback, ticket.Choice).Scan(
			&selected.id, &selected.pair, &selected.role, &selected.version, &selected.expiresAt, &delivered,
		)
		if err != nil || delivered {
			return selected, err
		}
		if attempt == maxReceiptRetries {
			return passIntakeSelection{}, ErrRoutingPending
		}
		select {
		case <-ctx.Done():
			return passIntakeSelection{}, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * 10 * time.Millisecond):
		}
	}
}

// PassTicketPriority reads the position accepted with the Telegram update.
// Worker delays, retries and form navigation can never start or extend a hold.
func (s *Store) PassTicketPriority(
	ctx context.Context,
	scope PaymentScope,
	eventID uuid.UUID,
	role string,
) (PromoPriority, error) {
	var prior PromoPriority
	err := s.pool.QueryRow(ctx, `
 SELECT c.tier_id,c.selected_at,c.expires_at FROM shuttle_pass_promo_choices c
 JOIN shuttle_events e ON e.id=c.event_id JOIN shuttle_users u ON u.id=c.user_id
 WHERE c.event_id=$1 AND c.user_id=$2 AND e.bot_id=$3 AND e.organization_id=$4 AND u.bot_id=$3
 AND u.zitadel_org=$4 AND u.zitadel_subject=$5 AND NOT u.revoked AND NOT c.forfeited
   AND c.expires_at>clock_timestamp() AND (c.eligible_role IS NULL OR $6='' OR c.eligible_role=$6)
 ORDER BY c.selected_at, c.sequence LIMIT 1
`, eventID, scope.UserID, scope.BotID, scope.Organization, scope.Subject, role).Scan(
		&prior.TierID, &prior.SelectedAt, &prior.ExpiresAt,
	)
	return prior, err
}

func (s *Store) savePassIntakeChoice(
	ctx context.Context,
	tx pgx.Tx,
	selected *passIntakeSelection,
	owner sqlc.ShuttleUser,
	scope EventScope,
) error {
	if owner.ZitadelOrg != "" && owner.ZitadelOrg != scope.Organization {
		return nil
	}
	if selected.pair {
		return releasePassIntakePriority(ctx, tx, *selected, owner.ID)
	}
	err := selectPassTicketTx(ctx, tx, selected.event, owner.ID, selected.role)
	if errors.Is(err, registrations.ErrUnavailable) {
		return nil
	}
	return err
}

// An incomplete pair application cannot keep a solo hold. Completed registrations
// retain their priority until a canonical invitation operation changes it.
func releasePassIntakePriority(ctx context.Context, tx pgx.Tx, selected passIntakeSelection, userID uuid.UUID) error {
	q := sqlc.New(tx)
	if selected.expiresAt != nil {
		now, err := q.RegistrationNow(ctx)
		if err != nil || !now.Time.Before(*selected.expiresAt) {
			return err
		}
	}
	_, err := q.GetRegistration(ctx, sqlc.GetRegistrationParams{EventID: selected.id, UserID: userID})
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return relinquishPassPromo(ctx, tx, selected.id, userID)
}
