package core

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// PrivilegedReadCapabilities is live role evidence, never a grant to read an event.
type PrivilegedReadCapabilities struct {
	PaymentReads      bool `json:"payment_reads"`
	PractitionerReads bool `json:"practitioner_reads"`
}

type PrivilegedReadEvent struct {
	PrivilegedReadCapabilities

	Event string `json:"event"`
}

func (s Service) PrivilegedReads(ctx context.Context, actor string) (PrivilegedReadCapabilities, error) {
	var result PrivilegedReadCapabilities
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_payment_admins WHERE owner=$1),
 EXISTS(SELECT 1 FROM core.massage_specialists WHERE owner=$1) FROM core.users WHERE id=$1`, actor).
		Scan(&result.PaymentReads, &result.PractitionerReads)
	return result, err
}

// PrivilegedReadEvents includes historical event scopes; each page reads current roles.
func (s Service) PrivilegedReadEvents(ctx context.Context, actor, raw string) (ReadPage[PrivilegedReadEvent], error) {
	capabilities, err := s.PrivilegedReads(ctx, actor)
	if err != nil {
		return ReadPage[PrivilegedReadEvent]{}, err
	}
	if !capabilities.PaymentReads && !capabilities.PractitionerReads {
		return ReadPage[PrivilegedReadEvent]{}, &ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	cursor, err := DecodeReadCursor(raw, actor, "privileges.events")
	if err != nil {
		return ReadPage[PrivilegedReadEvent]{}, err
	}
	rows, err := s.DB.Query(ctx, `SELECT event_id,bool_or(payment),bool_or(practitioner) FROM (
 SELECT event_id,true AS payment,false AS practitioner FROM core.pass_payment_admins WHERE owner=$1
 UNION ALL SELECT event_id,false,true FROM core.massage_specialists WHERE owner=$1
 ) scopes WHERE event_id>$2 GROUP BY event_id ORDER BY event_id LIMIT $3`, actor, cursor.Position, ReadPageItems+1)
	if err != nil {
		return ReadPage[PrivilegedReadEvent]{}, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PrivilegedReadEvent, error) {
		var item PrivilegedReadEvent
		scanErr := row.Scan(&item.Event, &item.PaymentReads, &item.PractitionerReads)
		return item, scanErr
	})
	if err != nil {
		return ReadPage[PrivilegedReadEvent]{}, err
	}
	return NavigationPage(items, cursor, func(item PrivilegedReadEvent) string { return item.Event })
}
