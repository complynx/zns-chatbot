// Package adminutilities exposes the global administrator's diagnostic utilities.
package adminutilities

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type Service struct{ DB *pgxpool.Pool }

// Authorize uses the same global administrator mapping as administrative sends.
// Payment administrators and user-supplied text never confer these rights.
func (s Service) Authorize(ctx context.Context, actor string) error {
	var allowed bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$1)`, actor).
		Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return &core.ProblemError{Status: http.StatusForbidden, Code: "forbidden"}
	}
	return nil
}

type Report struct {
	TelegramID     int64             `json:"telegram_id"`
	Found          bool              `json:"found"`
	Names          map[string]string `json:"names"`
	Passes         []json.RawMessage `json:"passes"`
	States         map[string]int64  `json:"states"`
	Registered     []string          `json:"registered"`
	Paid           []string          `json:"paid"`
	Requests       int64             `json:"requests"`
	RecentRequests int64             `json:"recent_requests"`
	Replies        int64             `json:"replies"`
	LatestRequest  *time.Time        `json:"latest_request,omitempty"`
}

func (s Service) User(ctx context.Context, actor string, telegramID int64) (Report, error) {
	result := Report{TelegramID: telegramID, Names: map[string]string{}, Passes: []json.RawMessage{}}
	if err := s.Authorize(ctx, actor); err != nil {
		return result, err
	}
	var owner string
	err := s.DB.QueryRow(ctx, `SELECT u.id,jsonb_build_object('name',u.name,'username',u.username,
 'first_name',u.first_name,'last_name',u.last_name,'print_name',u.print_name,'legal_name',COALESCE(p.legal_name,''))
 FROM core.users u LEFT JOIN core.pass_profiles p ON p.owner=u.id WHERE u.telegram_id=$1`, telegramID).Scan(&owner, &result.Names)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	result.Found = true
	rows, err := s.DB.Query(ctx, `SELECT jsonb_build_object('event',b.event_id,'titles',e.titles,'state',b.state,
 'role',b.role,'type',b.kind,'price',b.price,'created_at',b.created_at,'assigned_at',b.assigned_at,
 'payment_admin',b.payment_admin,'proof_received',a.received_at,'proof_reviewed',a.reviewed_at,
 'proof_decision',a.decision,'proof_receiving_admin',a.receiving_admin,'proof_reviewing_admin',a.reviewed_by)
 FROM core.pass_bookings b JOIN core.pass_events e ON e.id=b.event_id
 LEFT JOIN core.pass_payment_attempts a ON a.id=b.payment_attempt
 WHERE b.owner=$1 ORDER BY b.event_id,b.created_at`, owner)
	if err != nil {
		return result, err
	}
	result.Passes, err = pgx.CollectRows(rows, pgx.RowTo[json.RawMessage])
	if err != nil {
		return result, err
	}
	result.States = map[string]int64{}
	result.Registered = []string{}
	result.Paid = []string{}
	for _, raw := range result.Passes {
		var pass struct {
			Event string `json:"event"`
			State string `json:"state"`
		}
		if err = json.Unmarshal(raw, &pass); err != nil {
			return result, err
		}
		result.States[pass.State]++
		result.Registered = append(result.Registered, pass.Event)
		if pass.State == "paid" {
			result.Paid = append(result.Paid, pass.Event)
		}
	}
	err = s.DB.QueryRow(ctx, `SELECT count(*) FILTER(WHERE kind='user'),
 count(*) FILTER(WHERE kind='user' AND created_at>=clock_timestamp()-interval '24 hours'),
 count(*) FILTER(WHERE kind='assistant'),max(created_at) FILTER(WHERE kind='user')
 FROM core.conversation_events WHERE owner=$1`, owner).Scan(&result.Requests, &result.RecentRequests, &result.Replies, &result.LatestRequest)
	return result, err
}

type Events struct {
	All     []string `json:"all"`
	Active  []string `json:"active"`
	Changed int64    `json:"changed"`
}

// Refresh reads current SQL configuration and runs the existing queue/deadline
// maintenance. There is no process-local event cache to invalidate in Go.
func (s Service) Refresh(ctx context.Context, actor string) (Events, error) {
	result := Events{All: []string{}, Active: []string{}}
	if err := s.Authorize(ctx, actor); err != nil {
		return result, err
	}
	changed, err := (passbooking.Service{DB: s.DB}).ProcessDeadlines(ctx)
	if err != nil {
		return result, err
	}
	result.Changed = changed
	rows, err := s.DB.Query(ctx, `SELECT id,finishes_at>clock_timestamp() FROM core.pass_events ORDER BY id`)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var active bool
		if err = rows.Scan(&id, &active); err != nil {
			return result, err
		}
		result.All = append(result.All, id)
		if active {
			result.Active = append(result.Active, id)
		}
	}
	return result, rows.Err()
}
