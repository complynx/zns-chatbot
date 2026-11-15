package db

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/broadcasts"
)

const communicationPersonal = "personal"
const communicationCancel = "cancel"
const communicationCancelled = "cancelled"

type CommunicationArguments struct {
	EventID          uuid.UUID           `json:"event_id,omitempty"`
	Recipients       []uuid.UUID         `json:"recipients" minItems:"1" maxItems:"1000"`
	Text             string              `json:"text" minLength:"1" maxLength:"2800"`
	Category         string              `json:"category" enum:"personal,advertising,information,warning"`
	AssessedCategory broadcasts.Category `json:"assessed_category,omitempty" enum:"advertising,information,warning"`
	Assessment       string              `json:"assessment,omitempty" maxLength:"256"`
	Personalized     bool                `json:"personalized,omitempty"`
}

type Communication struct {
	ID             uuid.UUID              `json:"id"`
	AuthorID       uuid.UUID              `json:"author_id"`
	Arguments      CommunicationArguments `json:"arguments"`
	Category       string                 `json:"category"`
	State          string                 `json:"state"`
	ExpiresAt      time.Time              `json:"expires_at"`
	DeliveryCounts map[string]int         `json:"delivery_counts,omitempty"`
}

func validCommunication(a CommunicationArguments) bool {
	if len(a.Recipients) < 1 || len(a.Recipients) > 1000 || !broadcasts.ValidText(a.Text) {
		return false
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range a.Recipients {
		if id == uuid.Nil || seen[id] {
			return false
		}
		seen[id] = true
	}
	if a.Category == communicationPersonal {
		return len(a.Recipients) == 1 && !a.Personalized && a.Assessment == "" && a.AssessedCategory == ""
	}
	return broadcasts.Category(a.Category).Valid() && a.AssessedCategory.Valid() &&
		strings.TrimSpace(a.Assessment) != "" &&
		utf8.RuneCountInString(a.Assessment) <= 256
}

// communicationAccess is intentionally stricter than public directory visibility.
// The caller has already locked the deployment event and actor.
func communicationAccess(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	event, recipient uuid.UUID,
	personal, targetAdmin bool,
) error {
	return communicationRecipientsAccess(ctx, tx, scope, event, []uuid.UUID{recipient}, personal, targetAdmin)
}

func communicationRecipientsAccess(
	ctx context.Context,
	tx pgx.Tx,
	scope PaymentScope,
	event uuid.UUID,
	recipients []uuid.UUID,
	personal, targetAdmin bool,
) error {
	var allowed int
	err := tx.QueryRow(
		ctx,
		`
SELECT count(*) FROM shuttle_users u
 WHERE u.id=ANY($3::uuid[]) AND u.bot_id=$1 AND u.zitadel_org=$2 AND NOT u.revoked AND
u.zitadel_subject<>''
 AND ($4 OR EXISTS(SELECT 1 FROM shuttle_event_authorities a JOIN
shuttle_registrations r ON
r.event_id=a.event_id
 WHERE a.event_id=$5 AND a.user_id=$6 AND a.role IN ('ambassador','administrator')
AND r.user_id=u.id)
 OR EXISTS(
`+peopleMassageClientSQL+` AND b.event_id=$5 AND sb.user_id=$6)
 OR ($7 AND ($8 OR EXISTS(SELECT 1 FROM shuttle_event_authorities a
 JOIN shuttle_registrations r ON r.event_id=a.event_id WHERE a.event_id=$5
 AND a.user_id=u.id AND a.role IN ('ambassador','administrator') AND r.user_id=$6))))`,
		scope.BotID,
		scope.Organization,
		recipients,
		scope.GlobalAdmin,
		optionalUUID(event),
		scope.UserID,
		personal,
		targetAdmin,
	).Scan(&allowed)
	if err == nil && allowed != len(recipients) {
		return ErrPeopleDenied
	}
	return err
}

// PrepareCommunication fixes the explicit recipient set and the agent's assessment.
// Only the author's native confirmation callback can queue the messages.
func (s *Store) PrepareCommunication(
	ctx context.Context,
	scope PaymentScope,
	op uuid.UUID,
	a CommunicationArguments,
	targetAdmin bool,
) (Communication, error) {
	var result Communication
	if !validCommunication(a) || op == uuid.Nil || !paymentScopeValid(scope, op.String()) {
		return result, broadcasts.ErrInvalid
	}
	// Ordinary participants can contact their event authorities without staff directory access.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockCommunicationActor(ctx, tx, scope, a.EventID); err != nil {
		return result, err
	}
	if err = communicationRecipientsAccess(
		ctx,
		tx,
		scope,
		a.EventID,
		a.Recipients,
		a.Category == communicationPersonal,
		targetAdmin,
	); err != nil {
		return result, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return result, err
	}
	var prior []byte
	err = tx.QueryRow(ctx, `
INSERT INTO
shuttle_communications(id,bot_id,organization_id,author_id,operation_id,event_id,arguments,category)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON
CONFLICT(bot_id,organization_id,author_id,operation_id)
 DO UPDATE SET operation_id=excluded.operation_id
 RETURNING id,author_id,arguments,category,state,expires_at
`, uuid.New(), scope.BotID, scope.Organization, scope.UserID, op, optionalUUID(a.EventID), raw, a.Category).
		Scan(&result.ID, &result.AuthorID, &prior, &result.Category, &result.State, &result.ExpiresAt)
	if err != nil {
		return result, err
	}
	var saved CommunicationArguments
	if err = json.Unmarshal(prior, &saved); err != nil {
		return result, err
	}
	normalized, _ := json.Marshal(saved)
	if !bytes.Equal(raw, normalized) {
		return result, ErrConflict
	}
	result.Arguments = saved
	_, err = tx.Exec(ctx, `
INSERT INTO shuttle_communication_deliveries(communication_id,recipient_id,kind)
 VALUES($1,$2,'confirmation') ON CONFLICT DO NOTHING
`, result.ID, scope.UserID)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func lockCommunicationActor(ctx context.Context, tx pgx.Tx, scope PaymentScope, event uuid.UUID) error {
	if event != uuid.Nil {
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
SELECT id FROM shuttle_events WHERE id=$1 AND bot_id=$2 AND organization_id=$3 FOR
SHARE
`, event, scope.BotID, scope.Organization).
			Scan(&id); err != nil {
			return err
		}
	}
	return lockProfileOwner(
		ctx,
		tx,
		ProfileScope{BotID: scope.BotID, Organization: scope.Organization, UserID: scope.UserID, Subject: scope.Subject},
		false,
	)
}

func readCommunication(
	ctx context.Context,
	tx pgx.Tx,
	scope EventScope,
	id uuid.UUID,
	lock bool,
) (Communication, error) {
	var c Communication
	var raw []byte
	sql := `
SELECT id,author_id,arguments,category,state,expires_at FROM shuttle_communications
WHERE id=$1
 AND bot_id=$2 AND organization_id=$3
`
	if lock {
		sql += " FOR UPDATE"
	}
	err := tx.QueryRow(ctx, sql, id, scope.BotID, scope.Organization).
		Scan(&c.ID, &c.AuthorID, &raw, &c.Category, &c.State, &c.ExpiresAt)
	if err == nil {
		err = json.Unmarshal(raw, &c.Arguments)
	}
	return c, err
}

func (s *Store) Communication(ctx context.Context, scope PaymentScope, id uuid.UUID) (Communication, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Communication{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(
		ctx,
		tx,
		ProfileScope{BotID: scope.BotID, Organization: scope.Organization, UserID: scope.UserID, Subject: scope.Subject},
		false,
	); err != nil {
		return Communication{}, err
	}
	c, err := readCommunication(ctx, tx, scope.EventScope, id, false)
	if err == nil && c.AuthorID != scope.UserID {
		return Communication{}, ErrPeopleDenied
	}
	if err == nil {
		rows, e := tx.Query(
			ctx,
			`
SELECT state,count(*) FROM shuttle_communication_deliveries WHERE communication_id=$1
 AND
kind='message' GROUP BY state
`,
			id,
		)
		if e != nil {
			return c, e
		}
		defer rows.Close()
		c.DeliveryCounts = map[string]int{}
		for rows.Next() {
			var state string
			var count int
			if e = rows.Scan(&state, &count); e != nil {
				return c, e
			}
			c.DeliveryCounts[state] = count
		}
		err = rows.Err()
	}
	return c, err
}

// DecideCommunication consumes one native decision. A repeated click is idempotent.
func (s *Store) DecideCommunication(
	ctx context.Context,
	scope PaymentScope,
	id uuid.UUID,
	decision string,
	targetAdmin bool,
) (Communication, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Communication{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	c, err := readCommunication(ctx, tx, scope.EventScope, id, false)
	if err != nil {
		return c, err
	}
	if c.AuthorID != scope.UserID {
		return c, ErrPeopleDenied
	}
	if err = lockCommunicationActor(ctx, tx, scope, c.Arguments.EventID); err != nil {
		return c, err
	}
	c, err = readCommunication(ctx, tx, scope.EventScope, id, true)
	if err != nil {
		return c, err
	}
	if c.State != "prepared" {
		return c, nil
	}
	if !time.Now().Before(c.ExpiresAt) {
		return c, ErrConflict
	}
	switch decision {
	case communicationCancel:
		c.State = communicationCancelled
	case "keep", "suggested":
		if decision == "suggested" {
			if !broadcasts.NeedsClarification(broadcasts.Category(c.Category), c.Arguments.AssessedCategory) {
				return c, broadcasts.ErrInvalid
			}
			c.Category = string(c.Arguments.AssessedCategory)
		}
		if err = queueCommunication(ctx, tx, scope, c, targetAdmin); err != nil {
			return c, err
		}
		c.State = "queued"
	default:
		return c, broadcasts.ErrInvalid
	}
	_, err = tx.Exec(ctx, `
UPDATE shuttle_communications SET state=$2,category=$3 WHERE id=$1
`, id, c.State, c.Category)
	if err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}

func subscriptionScope(event uuid.UUID) string {
	if event == uuid.Nil {
		return "general"
	}
	return event.String()
}

// SetBroadcastSubscription accepts only optional categories. Event and global opt-outs are additive.
func (s *Store) SetBroadcastSubscription(
	ctx context.Context,
	scope ProfileScope,
	event uuid.UUID,
	all bool,
	category broadcasts.Category,
	subscribed bool,
) error {
	if !category.Optional() {
		return broadcasts.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`
SELECT pg_advisory_xact_lock(hashtextextended($1,0))
`,
		"broadcast-subscription:"+scope.UserID.String(),
	); err != nil {
		return err
	}
	key := subscriptionScope(event)
	if all {
		key = "all"
	}
	_, err = tx.Exec(
		ctx,
		`
INSERT INTO shuttle_broadcast_subscriptions(user_id,scope,category,subscribed)
VALUES($1,$2,$3,$4)
 ON CONFLICT(user_id,scope,category) DO UPDATE SET
subscribed=excluded.subscribed,changed_at=clock_timestamp()
`,
		scope.UserID,
		key,
		category,
		subscribed,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func communicationSubscribed(ctx context.Context, tx pgx.Tx, user, event uuid.UUID, category string) (bool, error) {
	if category == "warning" || category == communicationPersonal {
		return true, nil
	}
	var allowed bool
	err := tx.QueryRow(ctx, `
SELECT NOT EXISTS(SELECT 1 FROM shuttle_broadcast_subscriptions WHERE user_id=$1 AND
category=$2 AND scope IN ('all',$3) AND NOT subscribed)
`, user, category, subscriptionScope(event)).
		Scan(&allowed)
	return allowed, err
}

func queueCommunication(ctx context.Context, tx pgx.Tx, scope PaymentScope, c Communication, targetAdmin bool) error {
	if err := communicationRecipientsAccess(
		ctx,
		tx,
		scope,
		c.Arguments.EventID,
		c.Arguments.Recipients,
		c.Category == communicationPersonal,
		targetAdmin,
	); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO shuttle_communication_deliveries(communication_id,recipient_id,kind)
SELECT $1,id,'message' FROM unnest($2::uuid[]) recipients(id) ON CONFLICT DO NOTHING`, c.ID, c.Arguments.Recipients)
	return err
}
