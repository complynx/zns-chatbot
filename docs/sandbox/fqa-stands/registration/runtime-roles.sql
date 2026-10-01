-- Run as the real owning zns_app after genuine migrations and fixture init.
-- No role membership, ownership transfer, default grants or PUBLIC permissions.
DO $$
BEGIN
 IF current_database()<>'synthetic_qa_zns_registration_fixture'
 OR current_user<>'zns_app' OR session_user<>'zns_app'
 OR EXISTS(SELECT 1 FROM pg_roles WHERE rolname=current_user
 AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls))
 OR NOT EXISTS(SELECT 1 FROM pg_database WHERE datname=current_database()
 AND pg_get_userbyid(datdba)='zns_app')
 OR NOT EXISTS(SELECT 1 FROM pg_class WHERE oid='public.zns_sandbox_fixtures'::regclass
 AND pg_get_userbyid(relowner)='zns_app')
 OR NOT has_table_privilege('zns_app','public.zns_sandbox_fixtures','SELECT')
 OR NOT has_table_privilege('zns_app','public.zns_sandbox_fixtures','INSERT')
 OR (SELECT count(*) FROM public.zns_sandbox_fixtures
 WHERE name IN ('product-v1','product-passport-v1','registration-fqa-v1'))<>3
 OR (SELECT count(*) FROM core.users)<>3
 OR (SELECT count(*) FROM core.users WHERE (id='alice' AND telegram_id=101)
 OR (id='bob' AND telegram_id=202) OR (id='visitor' AND telegram_id=303))<>3
 THEN RAISE EXCEPTION 'registration runtime role allocation guard failed';
 END IF;
END $$;
GRANT USAGE ON SCHEMA bot TO zns_fake;
GRANT SELECT, INSERT, UPDATE ON bot.fake_state TO zns_fake;
GRANT SELECT, INSERT ON bot.fake_files TO zns_fake;
GRANT SELECT ON bot.cursors, bot.interactions TO zns_fake;
GRANT USAGE ON SCHEMA public, core TO zns_registration_operator;
GRANT SELECT ON public.zns_sandbox_fixtures, core.users, core.pass_events,
 core.pass_bookings, core.registration_intents, core.registration_ingress,
 core.pass_payment_admins, core.pass_booking_admins TO zns_registration_operator;
GRANT SELECT(event_id, position, starts_at) ON core.pass_event_tiers TO zns_registration_operator;
GRANT INSERT, DELETE ON core.pass_payment_admins, core.pass_booking_admins TO zns_registration_operator;
-- PostgreSQL row locks require UPDATE permission on at least one column.
GRANT UPDATE(id) ON core.users, core.pass_events TO zns_registration_operator;
GRANT UPDATE(owner) ON core.pass_payment_admins, core.pass_booking_admins TO zns_registration_operator;
