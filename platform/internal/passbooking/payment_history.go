package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// PaymentHistoryEntry describes one attempt/participant snapshot, including
// replaced attempts. It has no current booking version or raw proof attachment.
type PaymentHistoryEntry struct {
	Attempt          string     `json:"attempt"`
	Event            string     `json:"event"`
	Kind             string     `json:"kind"`
	Participant      string     `json:"participant"`
	AssignedAt       *time.Time `json:"assigned_at,omitempty"`
	Submitter        *string    `json:"submitter"`
	ReceivingAdmin   string     `json:"receiving_admin"`
	ReceivedAt       time.Time  `json:"received_at"`
	Decision         string     `json:"decision"`
	ReviewedBy       *string    `json:"reviewed_by"`
	ReviewedAt       *time.Time `json:"reviewed_at"`
	ProofAvailable   bool       `json:"proof_available"`
	ProofUnavailable bool       `json:"proof_unavailable"`
}

type paymentHistoryBoundary struct {
	At          time.Time `json:"at"`
	Attempt     string    `json:"attempt"`
	Participant string    `json:"participant"`
}

func (s Service) PaymentHistoryPage(
	ctx context.Context,
	actor, event, raw string,
) (core.ReadPage[PaymentHistoryEntry], error) {
	cursor, err := core.DecodeReadCursor(raw, actor, "passes.payments.history:"+event)
	if err != nil {
		return core.ReadPage[PaymentHistoryEntry]{}, err
	}
	var boundary paymentHistoryBoundary
	if cursor.Position != "" &&
		(json.Unmarshal([]byte(cursor.Position), &boundary) != nil || boundary.At.IsZero() || boundary.Attempt == "") {
		return core.ReadPage[PaymentHistoryEntry]{}, core.ReadProblem("read_cursor_invalid")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return core.ReadPage[PaymentHistoryEntry]{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var owner string
	err = tx.QueryRow(ctx, `SELECT owner FROM core.pass_payment_admins WHERE event_id=$1 AND owner=$2 FOR SHARE`, event, actor).
		Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return core.ReadPage[PaymentHistoryEntry]{}, forbidden()
	}
	if err != nil {
		return core.ReadPage[PaymentHistoryEntry]{}, err
	}
	rows, err := tx.Query(ctx, `SELECT p.id,p.event_id,p.kind,COALESCE(member.owner,''),member.assigned_at,p.submitter,
 COALESCE(legacy.receiving_admin,p.receiving_admin,''),p.received_at,p.decision,
 COALESCE(p.reviewed_by,legacy.reviewed_by),p.reviewed_at,p.proof_id IS NOT NULL,p.proof_unavailable
 FROM core.pass_payment_attempts p
 LEFT JOIN core.pass_payment_participants member ON member.attempt=p.id
 LEFT JOIN core.legacy_pass_payment_metadata legacy ON p.legacy_source_key IS NOT NULL
 AND legacy.event_id=p.event_id AND legacy.owner=member.owner AND legacy.assigned_at=member.assigned_at
 AND legacy.received_at=p.received_at
 WHERE p.event_id=$1 AND ($2='' OR (p.received_at,p.id,COALESCE(member.owner,''))>($3,$4,$5))
 ORDER BY p.received_at,p.id,COALESCE(member.owner,'') LIMIT $6`, event, cursor.Position, boundary.At, boundary.Attempt, boundary.Participant, core.ReadPageItems+1)
	if err != nil {
		return core.ReadPage[PaymentHistoryEntry]{}, err
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[PaymentHistoryEntry])
	if err != nil {
		return core.ReadPage[PaymentHistoryEntry]{}, err
	}
	page, err := core.NavigationPage(items, cursor, func(item PaymentHistoryEntry) string {
		data, _ := json.Marshal(
			paymentHistoryBoundary{At: item.ReceivedAt, Attempt: item.Attempt, Participant: item.Participant},
		)
		return string(data)
	})
	if err != nil {
		return page, err
	}
	return page, tx.Commit(ctx)
}
