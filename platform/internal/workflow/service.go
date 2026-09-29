// Package workflow owns slot selection and booking transactions.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type Slot struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Capacity  int       `json:"capacity"`
	Remaining int       `json:"remaining"`
	Price     int       `json:"price"`
	Currency  string    `json:"currency"`
	StartsAt  time.Time `json:"starts_at"`
}
type Workflow struct {
	SlotID    string    `json:"slot_id"`
	Version   int64     `json:"version"`
	State     string    `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Action struct {
	Name    string `json:"name"`
	SlotID  string `json:"slot_id,omitempty"`
	Version int64  `json:"version"`
	Key     string `json:"key"`
	Origin  string `json:"origin"`
}
type Service struct {
	DB *pgxpool.Pool
	// Clock permits deterministic lock/deadline scenarios. The default reads
	// PostgreSQL wall time; never accept a clock from a request or user context.
	Clock func(context.Context, pgx.Tx) (time.Time, error)
}

func (s Service) now(ctx context.Context, tx pgx.Tx) (time.Time, error) {
	if s.Clock != nil {
		return s.Clock(ctx, tx)
	}
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now, err
}

func (s Service) Catalog(ctx context.Context) ([]Slot, error) {
	rows, e := s.DB.Query(
		ctx,
		`SELECT s.id,s.title,s.capacity,s.capacity-count(w.owner)::int,s.price,s.currency,s.starts_at FROM core.slots s LEFT JOIN core.workflows w ON w.slot_id=s.id AND w.state='booked' GROUP BY s.id ORDER BY s.id`,
	)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Slot{}
	for rows.Next() {
		var v Slot
		if e = rows.Scan(&v.ID, &v.Title, &v.Capacity, &v.Remaining, &v.Price, &v.Currency, &v.StartsAt); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s Service) Current(ctx context.Context, owner string) (Workflow, error) {
	var w Workflow
	e := s.DB.QueryRow(ctx, `SELECT slot_id,version,state,expires_at FROM core.workflows WHERE owner=$1`, owner).
		Scan(&w.SlotID, &w.Version, &w.State, &w.ExpiresAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return Workflow{State: "empty"}, nil
	}
	return w, e
}

const (
	actionSelect       = "select"
	stateDraft         = "draft"
	stateBooked        = "booked"
	draftLifetime      = 15 * time.Minute
	maxActionKeyLength = 128
	maxSlotIDLength    = 64
)

// Execute serializes each user's workflow, then locks the shared capacity row.
// Retry keys bind to the entire action, so replay cannot change its parameters.
func (s Service) Execute(ctx context.Context, owner string, a Action) (Workflow, error) {
	if err := a.validate(); err != nil {
		return Workflow{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Workflow{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup only; the operation reports its own error.
	prepared, err := s.PrepareWorkflowInTx(ctx, tx, owner, a)
	if err != nil {
		return Workflow{}, err
	}
	if result, found := prepared.Replay(); found {
		return result, nil
	}
	workflow, err := prepared.Apply(ctx)
	if err != nil {
		return workflow, err
	}
	if err = tx.Commit(ctx); err != nil {
		return workflow, fmt.Errorf("commit operation: %w", err)
	}
	return workflow, nil
}

func (a Action) validate() error {
	if a.Key == "" || len(a.Key) > maxActionKeyLength || len(a.SlotID) > maxSlotIDLength || a.Version < 0 {
		return fail(http.StatusBadRequest, "invalid_action")
	}
	if a.Origin != "manual" && a.Origin != "agent" {
		return fail(http.StatusBadRequest, "invalid_origin")
	}
	if a.Name != actionSelect && a.Name != "confirm" && a.Name != "cancel" {
		return fail(http.StatusBadRequest, "unknown_action")
	}
	return nil
}

func authorizeAction(ctx context.Context, tx pgx.Tx, owner string, a Action) error {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT can_book FROM core.users WHERE id=$1 FOR UPDATE`, owner).Scan(&allowed)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !allowed) {
		return fail(http.StatusForbidden, "forbidden")
	}
	if err != nil {
		return err
	}
	if a.Origin == "agent" && a.Name != actionSelect {
		return fail(http.StatusForbidden, "human_confirmation_required")
	}
	return nil
}

func replayAction(ctx context.Context, tx pgx.Tx, owner, key, hash string) (Workflow, bool, error) {
	var oldHash string
	var result Workflow
	err := tx.QueryRow(ctx, `SELECT request_hash,result FROM core.operations WHERE owner=$1 AND key=$2`, owner, key).
		Scan(&oldHash, &result)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if oldHash != hash {
		return Workflow{}, true, fail(http.StatusConflict, "idempotency_conflict")
	}
	return result, true, nil
}

func (s Service) loadWorkflow(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	version int64,
) (Workflow, time.Time, error) {
	var w Workflow
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT slot_id,version,state,expires_at FROM core.workflows WHERE owner=$1`, owner).
		Scan(&w.SlotID, &w.Version, &w.State, &w.ExpiresAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return w, now, err
	}
	if w.Version != version {
		return w, now, fail(http.StatusConflict, "stale_view")
	}
	now, err = s.now(ctx, tx)
	return w, now, err
}

func (s Service) applyAction(ctx context.Context, tx pgx.Tx, w *Workflow, a Action, now time.Time) error {
	switch a.Name {
	case actionSelect:
		return selectSlot(ctx, tx, w, a.SlotID, now)
	case "confirm":
		return s.confirmSlot(ctx, tx, w)
	case "cancel":
		if w.State != stateDraft && w.State != stateBooked {
			return fail(http.StatusConflict, "nothing_to_cancel")
		}
		w.State = "cancelled"
	}
	return nil
}

func selectSlot(ctx context.Context, tx pgx.Tx, w *Workflow, slot string, now time.Time) error {
	if w.State == stateBooked {
		return fail(http.StatusConflict, "already_booked")
	}
	var starts time.Time
	err := tx.QueryRow(ctx, `SELECT starts_at FROM core.slots WHERE id=$1`, slot).Scan(&starts)
	if errors.Is(err, pgx.ErrNoRows) {
		return fail(http.StatusNotFound, "slot_not_found")
	}
	if err != nil {
		return err
	}
	if !starts.After(now) {
		return fail(http.StatusConflict, "slot_closed")
	}
	w.SlotID = slot
	w.State = stateDraft
	w.ExpiresAt = now.Add(draftLifetime)
	return nil
}

func (s Service) confirmSlot(ctx context.Context, tx pgx.Tx, w *Workflow) error {
	if w.State != stateDraft {
		return fail(http.StatusConflict, "not_a_draft")
	}
	var capacity, count int
	var starts time.Time
	err := tx.QueryRow(ctx, `SELECT capacity,starts_at FROM core.slots WHERE id=$1 FOR UPDATE`, w.SlotID).
		Scan(&capacity, &starts)
	if err != nil {
		return err
	}
	now, err := s.now(ctx, tx)
	if err != nil {
		return err
	}
	if !w.ExpiresAt.After(now) {
		return fail(http.StatusConflict, "intent_expired")
	}
	if !starts.After(now) {
		return fail(http.StatusConflict, "slot_closed")
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FROM core.workflows WHERE slot_id=$1 AND state='booked'`, w.SlotID).
		Scan(&count)
	if err != nil {
		return err
	}
	if count >= capacity {
		return fail(http.StatusConflict, "sold_out")
	}
	w.State = stateBooked
	return nil
}

func saveAction(ctx context.Context, tx pgx.Tx, owner string, a Action, w Workflow, hash string) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.workflows(owner,slot_id,version,state,expires_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(owner) DO UPDATE SET slot_id=$2,version=$3,state=$4,expires_at=$5`,
		owner,
		w.SlotID,
		w.Version,
		w.State,
		w.ExpiresAt,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.operations(owner,key,request_hash,result) VALUES($1,$2,$3,$4)`,
		owner,
		a.Key,
		hash,
		w,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.audit(owner,origin,action,version) VALUES($1,$2,$3,$4)`,
		owner,
		a.Origin,
		a.Name,
		w.Version,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO core.outbox(owner) VALUES($1)`, owner)
	return err
}

func fail(status int, code string) error { return &core.ProblemError{Status: status, Code: code} }
