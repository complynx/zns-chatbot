package db

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const PassReturned = registrations.Returned

// AccountPassReturn shows a participant the financial outcome without a payable pass.
type AccountPassReturn struct {
	ReturnedAt       time.Time  `json:"returned_at"`
	FrozenPriceMinor int64      `json:"frozen_price_minor"`
	Currency         string     `json:"currency,omitempty"`
	CurrencyScale    int32      `json:"currency_scale"`
	ConfirmedMinor   int64      `json:"confirmed_minor"`
	RefundDueMinor   int64      `json:"refund_due_minor"`
	RefundedMinor    int64      `json:"refunded_minor"`
	RefundedAt       *time.Time `json:"refunded_at,omitempty"`
}

func readAccountPassReturn(ctx context.Context, tx pgx.Tx, registrationID uuid.UUID) (*AccountPassReturn, error) {
	var r AccountPassReturn
	err := tx.QueryRow(ctx, `SELECT ret.returned_at,COALESCE(p.price_minor,0),COALESCE(p.currency,''),
COALESCE(p.currency_scale,0),ret.confirmed_minor,ret.refund_due_minor,COALESCE(ref.amount_minor,0),ref.recorded_at
FROM shuttle_pass_returns ret LEFT JOIN shuttle_passes p USING(registration_id)
LEFT JOIN shuttle_pass_refunds ref USING(registration_id) WHERE ret.registration_id=$1`, registrationID).
		Scan(&r.ReturnedAt, &r.FrozenPriceMinor, &r.Currency, &r.CurrencyScale,
			&r.ConfirmedMinor, &r.RefundDueMinor, &r.RefundedMinor, &r.RefundedAt)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// CheckPassAdminExport rechecks access to the selected event after workbook generation.
func (s *Store) CheckPassAdminExport(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPairAssignmentSession,
	eventID uuid.UUID,
) error {
	tx, _, err := s.beginPairAssignmentRead(ctx, scope, eventID, session)
	if err != nil {
		return err
	}
	return tx.Rollback(ctx)
}

type PassAdminChange struct {
	OperationID     uuid.UUID `json:"operation_id"`
	RegistrationID  uuid.UUID `json:"registration_id"`
	ExpectedVersion int64     `json:"expected_version"`
	ConfirmedMinor  int64     `json:"confirmed_minor"`
	RefundMinor     int64     `json:"refund_minor"`
	Reason          string    `json:"reason"`
}

type PassAdminReceipt struct {
	RegistrationID uuid.UUID `json:"registration_id"`
	OperationID    uuid.UUID `json:"operation_id"`
	RefundMinor    int64     `json:"refund_minor"`
	RecordedAt     time.Time `json:"recorded_at"`
}

type PassAdminRow struct {
	RegistrationID        uuid.UUID  `json:"registration_id"`
	UserID                uuid.UUID  `json:"user_id"`
	TelegramID            int64      `json:"telegram_id"`
	DisplayName           string     `json:"display_name"`
	State                 string     `json:"state"`
	Role                  string     `json:"role"`
	Version               int64      `json:"version"`
	RequestedAt           time.Time  `json:"requested_at"`
	RolePosition          int64      `json:"role_position,omitempty"`
	PairPriorityAt        *time.Time `json:"pair_priority_at,omitempty"`
	PassID                *uuid.UUID `json:"pass_id,omitempty"`
	Tier                  string     `json:"tier,omitempty"`
	PartnerRegistrationID *uuid.UUID `json:"partner_registration_id,omitempty"`
	PriceMinor            int64      `json:"price_minor"`
	Currency              string     `json:"currency,omitempty"`
	CurrencyScale         int32      `json:"currency_scale"`
	ConfirmedMinor        int64      `json:"confirmed_minor"`
	PendingProof          bool       `json:"pending_proof"`
	RefundDueMinor        int64      `json:"refund_due_minor"`
	RefundedMinor         int64      `json:"refunded_minor"`
	ReturnedAt            *time.Time `json:"returned_at,omitempty"`
	ReturnedBy            *uuid.UUID `json:"returned_by,omitempty"`
	RefundedAt            *time.Time `json:"refunded_at,omitempty"`
	RefundedBy            *uuid.UUID `json:"refunded_by,omitempty"`
}

type PassAdminList struct {
	EventID                 uuid.UUID          `json:"event_id"`
	AsOf                    time.Time          `json:"as_of"`
	Rows                    []PassAdminRow     `json:"rows"`
	Payments                []PassAdminPayment `json:"-"`
	IdentityExportAvailable bool               `json:"identity_export_available,omitempty"`
}

type PassAdminPayment struct {
	EvidenceID     uuid.UUID
	RegistrationID uuid.UUID
	PassID         uuid.UUID
	Purpose        string
	ReportedMinor  int64
	Currency       string
	CurrencyScale  int32
	SubmittedAt    time.Time
	Decision       string
	ConfirmedMinor int64
	ReviewedAt     *time.Time
	ReviewedBy     *uuid.UUID
	ReviewerName   string
}

const registrationRosterSQL = passQueueRanksSQL + `SELECT r.id,r.user_id,u.tg_user_id,
 COALESCE(NULLIF(up.full_name,''),trim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name'))),
 r.state,r.role,r.version,r.requested_at,
 COALESCE(ranks.role_pos,0),
 ap.queued_at,p.id,COALESCE(t.tier_id,''),
 CASE WHEN ap.leader_registration_id=r.id THEN ap.follower_registration_id ELSE ap.leader_registration_id END,
 COALESCE(p.price_minor,0),COALESCE(p.currency,''),COALESCE(p.currency_scale,0),
 money.confirmed,money.pending,COALESCE(ret.refund_due_minor,0),COALESCE(ref.amount_minor,0),
 ret.returned_at,ret.actor_id,ref.recorded_at,ref.actor_id
 FROM shuttle_registrations r JOIN shuttle_users u ON u.id=r.user_id
 LEFT JOIN ranks ON ranks.id=r.id
 LEFT JOIN shuttle_user_profiles up ON up.user_id=u.id
 LEFT JOIN shuttle_passes p ON p.registration_id=r.id LEFT JOIN shuttle_pass_tiers t ON t.pass_id=p.id
 LEFT JOIN shuttle_active_pair_members m ON m.registration_id=r.id LEFT JOIN shuttle_pairs ap ON ap.id=m.pair_id
 LEFT JOIN shuttle_pass_returns ret ON ret.registration_id=r.id
 LEFT JOIN shuttle_pass_refunds ref ON ref.registration_id=r.id
 CROSS JOIN LATERAL (SELECT COALESCE(sum(v.amount_minor) FILTER(WHERE v.decision='confirmed'),0)::bigint confirmed,
 count(*) FILTER(WHERE v.id IS NULL)>0 pending FROM shuttle_payment_evidence e
 LEFT JOIN shuttle_payment_reviews v ON v.evidence_id=e.id WHERE e.pass_id=p.id) money
 WHERE r.event_id=$1 ORDER BY r.requested_at,r.id`

func (s *Store) PassAdminRows(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPairAssignmentSession,
	eventID uuid.UUID,
	includePayments bool,
) (PassAdminList, error) {
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, eventID, session)
	if err != nil {
		return PassAdminList{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return PassAdminList{}, err
	}
	list := PassAdminList{EventID: eventID, AsOf: now.Time.UTC(), Rows: []PassAdminRow{}}
	if !includePayments {
		policy, policyErr := passIdentityPolicy(ctx, q, scope.EventScope, eventID)
		if policyErr != nil {
			return PassAdminList{}, policyErr
		}
		list.IdentityExportAvailable = policy.available()
	}
	rows, err := tx.Query(ctx, registrationRosterSQL, eventID)
	if err != nil {
		return list, err
	}
	defer rows.Close()
	for rows.Next() {
		var r PassAdminRow
		if err = rows.Scan(
			&r.RegistrationID,
			&r.UserID,
			&r.TelegramID,
			&r.DisplayName,
			&r.State,
			&r.Role,
			&r.Version,
			&r.RequestedAt,
			&r.RolePosition,
			&r.PairPriorityAt,
			&r.PassID,
			&r.Tier,
			&r.PartnerRegistrationID,
			&r.PriceMinor,
			&r.Currency,
			&r.CurrencyScale,
			&r.ConfirmedMinor,
			&r.PendingProof,
			&r.RefundDueMinor,
			&r.RefundedMinor,
			&r.ReturnedAt,
			&r.ReturnedBy,
			&r.RefundedAt,
			&r.RefundedBy,
		); err != nil {
			return PassAdminList{}, err
		}
		list.Rows = append(list.Rows, r)
	}
	if err = rows.Err(); err != nil {
		return PassAdminList{}, err
	}
	if includePayments {
		payments, readErr := tx.Query(
			ctx,
			`SELECT e.id,p.registration_id,p.id,e.purpose,e.amount_minor,e.currency,e.currency_scale,e.submitted_at,
COALESCE(v.decision,'pending'),COALESCE(v.amount_minor,0),v.reviewed_at,v.actor_id,
COALESCE(NULLIF(profile.full_name,''),trim(concat_ws(' ',u.profile->>'first_name',u.profile->>'last_name')))
FROM shuttle_payment_evidence e JOIN shuttle_passes p ON p.id=e.pass_id
LEFT JOIN shuttle_payment_reviews v ON v.evidence_id=e.id LEFT JOIN shuttle_users u ON u.id=v.actor_id
LEFT JOIN shuttle_user_profiles profile ON profile.user_id=v.actor_id
WHERE e.event_id=$1 ORDER BY e.submitted_at,e.id`,
			eventID,
		)
		if readErr != nil {
			return PassAdminList{}, readErr
		}
		list.Payments, err = pgx.CollectRows(payments, pgx.RowToStructByPos[PassAdminPayment])
		if err != nil {
			return PassAdminList{}, err
		}
	}
	return list, authorizePairAssignmentMiniApp(ctx, q, scope, &session)
}

// ChangePassReturn serializes with assignments, pair changes and payment review under the event lock.
// The refund action records a completed external transfer against the approved return.
func (s *Store) ChangePassReturn(
	ctx context.Context,
	scope PaymentScope,
	session MiniAppPairAssignmentSession,
	eventID uuid.UUID,
	refund bool,
	c PassAdminChange,
) (PassAdminReceipt, error) {
	var result PassAdminReceipt
	c.Reason = strings.TrimSpace(c.Reason)
	if eventID == uuid.Nil || c.OperationID == uuid.Nil || c.RegistrationID == uuid.Nil || c.ExpectedVersion < 1 ||
		c.RefundMinor < 0 ||
		c.ConfirmedMinor < c.RefundMinor ||
		c.Reason == "" ||
		utf8.RuneCountInString(c.Reason) > 500 {
		return result, registrations.ErrInvalid
	}
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, eventID, session)
	if err != nil {
		return result, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	entry, err := q.GetRegistrationByID(ctx, sqlc.GetRegistrationByIDParams{ID: c.RegistrationID, EventID: eventID})
	if err != nil {
		return result, err
	}
	result = PassAdminReceipt{OperationID: c.OperationID, RegistrationID: c.RegistrationID, RefundMinor: c.RefundMinor}
	replaySQL := `SELECT returned_at FROM shuttle_pass_returns
WHERE actor_id=$1 AND operation_id=$2 AND registration_id=$3
AND confirmed_minor=$4 AND refund_due_minor=$5 AND reason=$6 AND expected_version=$7`
	if refund {
		replaySQL = `SELECT recorded_at FROM shuttle_pass_refunds f JOIN shuttle_pass_returns r USING(registration_id)
WHERE f.actor_id=$1 AND f.operation_id=$2 AND registration_id=$3
AND r.confirmed_minor=$4 AND f.amount_minor=$5 AND f.reason=$6 AND f.expected_version=$7`
	}
	err = tx.QueryRow(ctx, replaySQL, scope.UserID, c.OperationID, c.RegistrationID, c.ConfirmedMinor, c.RefundMinor,
		c.Reason, c.ExpectedVersion).
		Scan(&result.RecordedAt)
	if err == nil {
		return result, authorizePairAssignmentMiniApp(ctx, q, scope, &session)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return result, err
	}
	if entry.Version != c.ExpectedVersion {
		return result, ErrConflict
	}
	if refund {
		if entry.State != PassReturned || c.RefundMinor <= 0 {
			return result, ErrConflict
		}
		err = tx.QueryRow(ctx, `INSERT INTO shuttle_pass_refunds
(registration_id,actor_id,operation_id,amount_minor,reason,expected_version)
    SELECT registration_id,$2,$3,$4,$5,$7 FROM shuttle_pass_returns
WHERE registration_id=$1 AND refund_due_minor=$4 AND confirmed_minor=$6
    ON CONFLICT DO NOTHING RETURNING recorded_at`, entry.ID, scope.UserID, c.OperationID, c.RefundMinor,
			c.Reason, c.ConfirmedMinor, c.ExpectedVersion).
			Scan(&result.RecordedAt)
	} else {
		err = returnPass(ctx, tx, q, scope.UserID, entry, c, &result)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrConflict
	}
	if err != nil {
		return result, err
	}
	if err = authorizePairAssignmentMiniApp(ctx, q, scope, &session); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func returnPass(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	actor uuid.UUID,
	entry sqlc.ShuttleRegistration,
	c PassAdminChange,
	result *PassAdminReceipt,
) error {
	if err := checkPassReturnBalance(ctx, q, entry, c.ConfirmedMinor); err != nil {
		return err
	}
	var pairID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT pair_id FROM shuttle_active_pair_members WHERE registration_id=$1`, entry.ID).
		Scan(&pairID)
	if err == nil {
		pair, e := q.GetPair(ctx, sqlc.GetPairParams{ID: pairID, EventID: entry.EventID})
		if e != nil {
			return e
		}
		if _, e = dissolvePairMembership(ctx, q, pair); e != nil {
			return e
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE shuttle_pair_invitations SET state=CASE WHEN inviter_registration_id=$1 THEN 'revoked' ELSE 'declined' END,
 version=version+1,decided_at=clock_timestamp() WHERE event_id=$2 AND state='pending'
AND (inviter_registration_id=$1 OR target_user_id=$3)`,
		entry.ID,
		entry.EventID,
		entry.UserID,
	); err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE shuttle_pass_invites SET revoked=true WHERE event_id=$1 AND pair_id IS NULL AND NOT revoked
AND (inviter_registration_id=$2 OR claimant_user_id=$3)`,
		entry.EventID,
		entry.ID,
		entry.UserID,
	); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE shuttle_pass_promo_choices SET forfeited=true
WHERE event_id=$1 AND user_id=$2 AND awarded_role IS NULL AND NOT forfeited`, entry.EventID, entry.UserID); err != nil {
		return err
	}
	if _, err = tx.Exec(
		ctx,
		`UPDATE shuttle_registrations SET state='returned',version=version+1 WHERE id=$1`,
		entry.ID,
	); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `INSERT INTO shuttle_pass_returns
(registration_id,actor_id,operation_id,reason,expected_version,confirmed_minor,refund_due_minor)
 VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING RETURNING returned_at`,
		entry.ID, actor, c.OperationID, c.Reason, c.ExpectedVersion, c.ConfirmedMinor, c.RefundMinor).
		Scan(&result.RecordedAt)
}

func checkPassReturnBalance(
	ctx context.Context,
	q *sqlc.Queries,
	entry sqlc.ShuttleRegistration,
	confirmed int64,
) error {
	switch entry.State {
	case registrations.Assigned:
		pass, err := q.GetPass(ctx, entry.ID)
		if err != nil {
			return err
		}
		amount, err := q.PaymentTotal(ctx, pass.ID)
		if err != nil {
			return err
		}
		pending, err := q.PendingPassEvidence(ctx, pass.ID)
		if err != nil {
			return err
		}
		if amount != confirmed || pending != 0 {
			return ErrConflict
		}
	case registrations.Waitlisted:
		if confirmed != 0 {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	return nil
}
