package sandbox

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const RegistrationFixtureStand = "synthetic-qa-zns-registration-fixture"
const RegistrationFixtureDatabase = "synthetic_qa_zns_registration_fixture"
const RegistrationFixtureEventA = "registration-fixture-a"
const RegistrationFixtureEventB = "registration-fixture-b"

const registrationFixtureMarker = "registration-fqa-v1"

// RegistrationFixture is a schema-owner CLI control, never a model or HTTP grant.
// Only init accepts an opening time. Read and revoke cannot change any clock.
type RegistrationFixture struct {
	Stand   string
	Action  string
	OpensAt time.Time
}

func (f RegistrationFixture) validate() error {
	if f.Stand != RegistrationFixtureStand {
		return errors.New("registration fixture stand guard failed")
	}
	switch f.Action {
	case "init":
		if f.OpensAt.IsZero() {
			return errors.New("registration fixture opening is required")
		}
	case "read", "revoke-payment-a", "revoke-booking-admin":
		if !f.OpensAt.IsZero() {
			return errors.New("registration fixture opening is init-only")
		}
	default:
		return errors.New("unknown registration fixture action")
	}
	return nil
}

// RegistrationFixtureState omits names, contacts, passports, proof files and text.
// Each observation is from real tables; capability reads use current domain ACL.
type RegistrationFixtureState struct {
	ObservedAt time.Time                `json:"observed_at"`
	Rows       []RegistrationFixtureRow `json:"rows"`
}

type RegistrationFixtureRow struct {
	Event               string     `json:"event"`
	Owner               string     `json:"owner"`
	Actions             []string   `json:"actions"`
	BookingState        *string    `json:"booking_state,omitempty"`
	Version             *int64     `json:"version,omitempty"`
	CreatedAt           *time.Time `json:"created_at,omitempty"`
	InvitationStartedAt *time.Time `json:"invitation_started_at,omitempty"`
	IntentID            *int64     `json:"intent_id,omitempty"`
	IntentState         *string    `json:"intent_state,omitempty"`
	Generation          *int64     `json:"generation,omitempty"`
	SalesOpen           *bool      `json:"sales_open,omitempty"`
	CheckedAt           *time.Time `json:"checked_at,omitempty"`
	IngressID           *int64     `json:"ingress_id,omitempty"`
	ReceivedAt          *time.Time `json:"received_at,omitempty"`
	Position            *int64     `json:"position,omitempty"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"`
	RequeueCount        *int64     `json:"requeue_count,omitempty"`
}

// ApplyRegistrationFixture requires the dedicated database's owning role and
// all three original synthetic identities. Repeating init never restores grants.
func ApplyRegistrationFixture(
	ctx context.Context,
	db *pgxpool.Pool,
	f RegistrationFixture,
) (RegistrationFixtureState, error) {
	var state RegistrationFixtureState
	if err := f.validate(); err != nil {
		return state, err
	}
	tx, err := db.Begin(ctx)
	if err != nil {
		return state, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = registrationFixtureGuard(ctx, tx); err != nil {
		return state, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(918431003)`); err != nil {
		return state, err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS public.zns_sandbox_fixtures
 (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return state, err
	}
	var initialized bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM public.zns_sandbox_fixtures WHERE name=$1)`, registrationFixtureMarker).
		Scan(&initialized); err != nil {
		return state, err
	}
	switch {
	case f.Action == "init" && !initialized:
		err = initializeRegistrationFixture(ctx, tx, f.OpensAt)
	case !initialized:
		err = errors.New("registration fixture is not initialized")
	default:
		switch f.Action {
		case "revoke-payment-a":
			_, err = tx.Exec(
				ctx,
				`DELETE FROM core.pass_payment_admins WHERE event_id=$1 AND owner='bob'`,
				RegistrationFixtureEventA,
			)
		case "revoke-booking-admin":
			_, err = tx.Exec(ctx, `DELETE FROM core.pass_booking_admins WHERE owner='visitor'`)
		}
	}
	if err != nil {
		return state, err
	}
	if err = tx.Commit(ctx); err != nil {
		return state, err
	}
	return readRegistrationFixture(ctx, db)
}

func registrationFixtureGuard(ctx context.Context, tx pgx.Tx) error {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT current_database()=$1 AND current_user=pg_get_userbyid(datdba)
 AND (SELECT count(*) FROM core.users)=3
 AND (SELECT count(*) FROM core.users WHERE (id='alice' AND telegram_id=101)
 OR (id='bob' AND telegram_id=202) OR (id='visitor' AND telegram_id=303))=3
 FROM pg_database WHERE datname=current_database()`, RegistrationFixtureDatabase).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("registration fixture database or identity guard failed")
	}
	return nil
}

func initializeRegistrationFixture(ctx context.Context, tx pgx.Tx, opens time.Time) error {
	var clean bool
	err := tx.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM core.pass_bookings)
 AND NOT EXISTS(SELECT 1 FROM core.registration_intents)
 AND NOT EXISTS(SELECT 1 FROM core.pass_events WHERE id IN ($1,$2))
 AND ((NOT EXISTS(SELECT 1 FROM public.zns_sandbox_fixtures WHERE name='product-v1')
 AND NOT EXISTS(SELECT 1 FROM core.pass_booking_admins)
 AND NOT EXISTS(SELECT 1 FROM core.pass_payment_admins))
 OR (EXISTS(SELECT 1 FROM public.zns_sandbox_fixtures WHERE name='product-v1')
 AND (SELECT count(*) FROM core.pass_booking_admins)=1
 AND EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner='bob')
 AND (SELECT count(*) FROM core.pass_payment_admins)=2
 AND (SELECT count(*) FROM core.pass_payment_admins WHERE owner='bob'
 AND event_id IN ('sandbox-festival','sandbox-passport-pair'))=2))`, RegistrationFixtureEventA, RegistrationFixtureEventB).Scan(&clean)
	if err != nil {
		return err
	}
	if !clean {
		return errors.New("registration fixture requires a fresh registration database")
	}
	_, err = tx.Exec(ctx, `INSERT INTO core.pass_events(id,finishes_at,titles,assignment_rule,amount_cap_per_role)
 VALUES($1,$3::timestamptz+interval '7 days','{"en":"Registration A","ru":"Регистрация A"}','paired',1),
 ($2,$3::timestamptz+interval '7 days','{"en":"Registration B","ru":"Регистрация B"}','distributed',1)`, RegistrationFixtureEventA, RegistrationFixtureEventB, opens)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES($1,0,2,100,$3),($1,1,2,150,$3),($2,0,2,100,$3)`, RegistrationFixtureEventA, RegistrationFixtureEventB, opens)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `DELETE FROM core.pass_booking_admins WHERE owner IN ('alice','bob','visitor');
 INSERT INTO core.pass_booking_admins(owner) VALUES('visitor');
 DELETE FROM core.pass_payment_admins WHERE owner IN ('alice','bob','visitor')`)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES($1,'bob')`,
		RegistrationFixtureEventA,
	)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO public.zns_sandbox_fixtures(name) VALUES($1)`, registrationFixtureMarker)
	return err
}

func readRegistrationFixture(ctx context.Context, db *pgxpool.Pool) (RegistrationFixtureState, error) {
	state := RegistrationFixtureState{Rows: []RegistrationFixtureRow{}}
	if err := db.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&state.ObservedAt); err != nil {
		return state, err
	}
	rows, err := db.Query(ctx, `SELECT e.id,u.id,b.state,b.version,b.created_at,b.invitation_started_at,
 i.id,i.state,i.generation,i.sales_open,i.checked_at,i.ingress_id,g.received_at,i.effective_position,i.turn_expires_at,i.requeue_count
 FROM core.pass_events e CROSS JOIN core.users u
 LEFT JOIN core.pass_bookings b ON b.event_id=e.id AND b.owner=u.id
 LEFT JOIN LATERAL (SELECT id,state,generation,sales_open,checked_at,ingress_id,effective_position,turn_expires_at,requeue_count
 FROM core.registration_intents ri WHERE ri.event_id=e.id AND ri.owner=u.id
 ORDER BY generation DESC LIMIT 1) i ON true
 LEFT JOIN core.registration_ingress g ON g.id=i.ingress_id
 WHERE e.id IN ($1,$2) AND u.id IN ('alice','bob','visitor') ORDER BY e.id,u.id LIMIT 6`, RegistrationFixtureEventA, RegistrationFixtureEventB)
	if err != nil {
		return state, err
	}
	defer rows.Close()
	for rows.Next() {
		var row RegistrationFixtureRow
		if err = rows.Scan(
			&row.Event,
			&row.Owner,
			&row.BookingState,
			&row.Version,
			&row.CreatedAt,
			&row.InvitationStartedAt,
			&row.IntentID,
			&row.IntentState,
			&row.Generation,
			&row.SalesOpen,
			&row.CheckedAt,
			&row.IngressID,
			&row.ReceivedAt,
			&row.Position,
			&row.ExpiresAt,
			&row.RequeueCount,
		); err != nil {
			return state, err
		}
		state.Rows = append(state.Rows, row)
	}
	if err = rows.Err(); err != nil {
		return state, err
	}
	rows.Close()
	service := passbooking.Service{DB: db}
	for index := range state.Rows {
		row := &state.Rows[index]
		capabilities, readErr := service.Capabilities(ctx, row.Owner, row.Event)
		if readErr != nil {
			return state, readErr
		}
		row.Actions = capabilities.Actions
	}
	return state, nil
}
