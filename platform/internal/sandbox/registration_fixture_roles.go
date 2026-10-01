package sandbox

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

const registrationOperatorRole = "zns_registration_operator"

func registrationFixtureOperator(ctx context.Context, tx pgx.Tx) (bool, error) {
	var operator bool
	err := tx.QueryRow(ctx, `SELECT current_user=$1`, registrationOperatorRole).Scan(&operator)
	return operator, err
}

// The private operator may use existing live actions only. Its database grants
// are a trusted SQL boundary, not an untrusted user or model capability.
func registrationFixtureOperatorGuard(ctx context.Context, tx pgx.Tx, f RegistrationFixture) error {
	if err := f.validate(); err != nil {
		return err
	}
	if f.Action == registrationFixtureInit {
		return errors.New("registration operator cannot initialize fixtures")
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT current_database()=$1
 AND pg_get_userbyid(d.datdba)='zns_app' AND current_user=$2 AND session_user=$2
 AND r.rolcanlogin AND NOT r.rolsuper AND NOT r.rolcreatedb AND NOT r.rolcreaterole
 AND NOT r.rolreplication AND NOT r.rolbypassrls
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=r.oid)
 FROM pg_database d CROSS JOIN pg_roles r
 WHERE d.datname=current_database() AND r.rolname=current_user`,
		RegistrationFixtureDatabase, registrationOperatorRole).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("registration operator database or login guard failed")
	}
	// Reads retain ACCESS SHARE table locks until commit; the shared fixture
	// advisory lock serializes genuine initialization and operator controls.
	if _, err = tx.Exec(ctx, `LOCK TABLE public.zns_sandbox_fixtures, core.users,
 core.pass_events, core.pass_bookings, core.registration_intents, core.registration_ingress,
 core.pass_payment_admins, core.pass_booking_admins IN ACCESS SHARE MODE`); err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT pg_get_userbyid(c.relowner)='zns_app'
 AND has_table_privilege('zns_app',c.oid,'SELECT')
 AND has_table_privilege('zns_app',c.oid,'INSERT')
 FROM pg_class c WHERE c.oid='public.zns_sandbox_fixtures'::regclass AND c.relkind='r'`).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("registration operator fixture ownership guard failed")
	}
	err = tx.QueryRow(ctx, `SELECT count(*)=7 AND bool_and(pg_get_userbyid(c.relowner)='zns_app')
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='core' AND c.relkind='r' AND c.relname IN
 ('users','pass_events','pass_bookings','registration_intents','registration_ingress',
 'pass_payment_admins','pass_booking_admins')`).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("registration operator core table ownership guard failed")
	}
	// Keep the domain event-before-actor order while verifying stable identities.
	if err = passbooking.LockMutationEvents(
		ctx,
		tx,
		[]string{RegistrationFixtureEventA, RegistrationFixtureEventB},
	); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `SELECT id FROM core.users ORDER BY id FOR SHARE`); err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM core.users)=3
 AND (SELECT count(*) FROM core.users WHERE (id='alice' AND telegram_id=101)
 OR (id='bob' AND telegram_id=202) OR (id='visitor' AND telegram_id=303))=3
 AND (SELECT count(*) FROM public.zns_sandbox_fixtures
 WHERE name IN ('product-v1','product-passport-v1',$1))=3`, registrationFixtureMarker).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return errors.New("registration operator identity or marker guard failed")
	}
	return nil
}
