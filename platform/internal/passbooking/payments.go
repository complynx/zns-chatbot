package passbooking

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// Payment keeps receiving and reviewing administrators separate from the current
// ambassador. An attempt and its participant snapshot survive replacement.
// ReceivedAt is nil when imported free-assignment metadata has no source timestamp.
type Payment struct {
	ProofUnavailable bool       `json:"proof_unavailable"`
	Kind             string     `json:"kind"`
	Attempt          string     `json:"attempt"`
	Event            string     `json:"event"`
	Submitter        *string    `json:"submitter"`
	ProofID          string     `json:"proof_id"`
	ReceivingAdmin   string     `json:"receiving_admin"`
	ReceivedAt       *time.Time `json:"received_at"`
	Decision         string     `json:"decision"`
	ReviewedBy       *string    `json:"reviewed_by,omitempty"`
	ReviewedAt       *time.Time `json:"reviewed_at,omitempty"`
	Version          int64      `json:"version"`
}

func (s *snapshot) submitProof(ctx context.Context, tx pgx.Tx, b *Booking, c Command) error {
	if b.State != assigned || c.ProofID == "" || len(c.ProofID) != 64 {
		return conflict("pass_payment_state")
	}
	var proof string
	err := tx.QueryRow(ctx, `SELECT id FROM core.order_proofs WHERE id=$1 AND owner=$2`, c.ProofID, b.Owner).
		Scan(&proof)
	if errors.Is(err, pgx.ErrNoRows) {
		return forbidden()
	}
	if err != nil {
		return err
	}
	participants := []*Booking{b}
	if b.Partner != "" {
		partner := s.bookings[b.Partner]
		if partner == nil || partner.Partner != b.Owner || partner.State != assigned {
			return conflict("pass_payment_state")
		}
		participants = append(participants, partner)
	}
	if err = authorizeTakeover(ctx, tx, b.PaymentAdmin, b.Event); err != nil {
		if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Code == "forbidden" {
			return conflict("pass_payment_admin_required")
		}
		return err
	}
	id := hash([]byte(c.Event + "\x00" + b.Owner + "\x00" + c.Key))
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_payment_attempts(id,event_id,submitter,proof_id,receiving_admin,received_at) VALUES($1,$2,$3,$4,$5,$6)`,
		id,
		c.Event,
		b.Owner,
		proof,
		b.PaymentAdmin,
		s.now,
	)
	if err != nil {
		return err
	}
	for _, participant := range participants {
		_, err = tx.Exec(
			ctx,
			`INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at) VALUES($1,$2,$3)`,
			id,
			participant.Owner,
			participant.AssignedAt,
		)
		if err != nil {
			return err
		}
		_, err = tx.Exec(
			ctx,
			`UPDATE core.pass_bookings SET payment_attempt=$3 WHERE event_id=$1 AND owner=$2`,
			c.Event,
			participant.Owner,
			id,
		)
		if err != nil {
			return err
		}
		participant.State = paid
		s.touch(participant)
	}
	return notifyPaymentRequest(ctx, tx, b, id)
}

func (s *snapshot) reviewProof(ctx context.Context, tx pgx.Tx, actor string, c Command) error {
	target := s.bookings[c.Target]
	if target == nil || target.Version != c.TargetVersion || target.State != paid || c.PaymentAttempt == "" {
		return conflict("pass_payment_stale")
	}
	var decision string
	err := tx.QueryRow(ctx, `SELECT p.decision FROM core.pass_payment_attempts p
JOIN core.pass_bookings b ON b.event_id=p.event_id AND b.payment_attempt=p.id
WHERE p.id=$1 AND p.event_id=$2 AND b.owner=$3`, c.PaymentAttempt, c.Event, c.Target).Scan(&decision)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && decision != "pending") {
		return conflict("pass_payment_stale")
	}
	if err != nil {
		return err
	}
	owners, err := s.paymentParticipants(ctx, tx, c)
	if err != nil {
		return err
	}
	decision = paymentAccepted
	if c.Name == commandProofReject {
		decision = "rejected"
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.pass_payment_attempts SET decision=$2,reviewed_by=$3,reviewed_at=$4 WHERE id=$1`,
		c.PaymentAttempt,
		decision,
		actor,
		s.now,
	)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		b := s.bookings[owner]
		if c.Name == commandProofReject {
			b.State = assigned
		}
		s.touch(b)
		if err = notifyPaymentDecision(ctx, tx, b, c); err != nil {
			return err
		}
	}
	return nil
}

func (s *snapshot) paymentParticipants(ctx context.Context, tx pgx.Tx, c Command) ([]string, error) {
	rows, err := tx.Query(ctx, `SELECT p.owner,COALESCE(b.payment_attempt=p.attempt,false),
COALESCE(b.state='paid' AND b.assigned_at=p.assigned_at,false)
FROM core.pass_payment_participants p LEFT JOIN core.pass_bookings b ON b.owner=p.owner AND b.event_id=$2
WHERE p.attempt=$1 ORDER BY p.owner`, c.PaymentAttempt, c.Event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var owners []string
	for rows.Next() {
		var owner string
		var attached, current bool
		if err = rows.Scan(&owner, &attached, &current); err != nil {
			return nil, err
		}
		// Cancellation or a new assignment detaches the old attempt. Reviewing
		// its surviving participant must not revive or alter the detached one.
		if !attached {
			continue
		}
		if !current || s.bookings[owner] == nil {
			return nil, conflict("pass_payment_stale")
		}
		owners = append(owners, owner)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(owners) == 0 {
		return nil, conflict("pass_payment_stale")
	}
	return owners, nil
}

// Payment returns only the current attachment to an authorized participant or
// an event payment administrator, including hidden administrators.
func (s Service) Payment(ctx context.Context, actor, eventID, owner string) (Payment, error) {
	var payment Payment
	err := s.DB.QueryRow(ctx, `SELECT p.kind,p.id,p.event_id,p.submitter,COALESCE(p.proof_id,''),COALESCE(legacy.receiving_admin,p.receiving_admin,''),p.received_at,
p.decision,COALESCE(p.reviewed_by,legacy.reviewed_by),p.reviewed_at,b.version,p.proof_unavailable
FROM core.pass_bookings b JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt
LEFT JOIN core.legacy_pass_payment_metadata legacy ON p.legacy_source_key IS NOT NULL
 AND legacy.event_id=b.event_id AND legacy.owner=b.owner AND legacy.assigned_at=b.assigned_at
 AND legacy.received_at=p.received_at
WHERE b.event_id=$1 AND b.owner=$2 AND b.state IN ('assigned','paid')
AND (b.owner=$3 OR EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=$1 AND a.owner=$3))`, eventID, owner, actor).
		Scan(&payment.Kind, &payment.Attempt, &payment.Event, &payment.Submitter, &payment.ProofID, &payment.ReceivingAdmin,
			&payment.ReceivedAt, &payment.Decision, &payment.ReviewedBy, &payment.ReviewedAt, &payment.Version, &payment.ProofUnavailable)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.legacyFreePayment(ctx, actor, eventID, owner)
	}
	return payment, err
}

// PaymentProof binds file authorization and current attachment in one statement.
func (s Service) PaymentProof(ctx context.Context, actor, eventID, owner string) (orders.Proof, error) {
	var proof orders.Proof
	err := s.DB.QueryRow(ctx, `SELECT f.id,f.filename,f.body,b.version,p.id
FROM core.pass_bookings b JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt
JOIN core.order_proofs f ON f.id=p.proof_id AND (f.owner=p.submitter OR
 (p.submitter IS NULL AND p.legacy_source_key IS NOT NULL AND EXISTS(
 SELECT 1 FROM core.legacy_pass_import_references r JOIN core.pass_payment_participants member ON member.attempt=p.id
 WHERE r.source_key=p.legacy_source_key AND r.event_id=p.event_id AND r.source_kind='proof' AND r.target_id=p.id AND member.owner=f.owner)))
WHERE b.event_id=$1 AND b.owner=$2 AND b.state IN ('assigned','paid')
AND (b.owner=$3 OR EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=$1 AND a.owner=$3))`, eventID, owner, actor).
		Scan(&proof.ID, &proof.Filename, &proof.Body, &proof.Version, &proof.Attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		payment, paymentErr := s.Payment(ctx, actor, eventID, owner)
		if paymentErr == nil && payment.ProofUnavailable {
			return orders.Proof{}, conflict("pass_receipt_unavailable")
		}
		return orders.Proof{}, forbidden()
	}
	return proof, err
}
