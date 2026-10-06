-- Read as the isolated registration operator after the seven bootstrap steps.
BEGIN READ ONLY;
SET LOCAL TIME ZONE 'UTC';
SELECT json_build_object(
 'database',current_database(),
 'session_user',session_user,
 'database_owner',(SELECT pg_get_userbyid(datdba) FROM pg_database
                   WHERE datname=current_database()),
 'fixture_owner',(SELECT pg_get_userbyid(relowner) FROM pg_class
                  WHERE oid='public.zns_sandbox_fixtures'::regclass),
 'actors',(SELECT json_agg(json_build_object('id',id,'telegram_id',telegram_id)
                         ORDER BY id) FROM core.users),
 'product_markers',(SELECT json_agg(name ORDER BY name)
                    FROM public.zns_sandbox_fixtures
                    WHERE name IN ('product-v1','product-passport-v1','registration-fqa-v1')),
 'booking_count',(SELECT count(*) FROM core.pass_bookings),
 'intent_count',(SELECT count(*) FROM core.registration_intents),
 'ingress_count',(SELECT count(*) FROM core.registration_ingress),
 'booking_admins',(SELECT json_agg(owner ORDER BY owner) FROM core.pass_booking_admins),
 'payment_admins',(SELECT json_agg(json_build_object('event_id',event_id,'owner',owner)
                                 ORDER BY event_id,owner) FROM core.pass_payment_admins),
 'registration_tiers',(SELECT json_agg(json_build_object('event_id',event_id,
                       'position',position,'starts_at',starts_at)
                       ORDER BY event_id,position) FROM core.pass_event_tiers
                       WHERE event_id IN ('registration-fixture-a','registration-fixture-b')),
 'allocated_roles',(SELECT json_agg(rolname ORDER BY rolname) FROM pg_roles
                     WHERE rolname IN ('zns_app','zns_fake','zns_registration_operator',
                                       'zns_meter','zns_inventory')),
 'role_memberships',(SELECT json_agg(json_build_object('member',member_role.rolname,
                     'role',granted_role.rolname,'admin',membership.admin_option)
                     ORDER BY member_role.rolname,granted_role.rolname)
                     FROM pg_auth_members membership
                     JOIN pg_roles member_role ON member_role.oid=membership.member
                     JOIN pg_roles granted_role ON granted_role.oid=membership.roleid
                     WHERE member_role.rolname IN ('zns_app','zns_fake','zns_registration_operator',
                                                   'zns_meter','zns_inventory')),
 'privileged_roles',(SELECT count(*) FROM pg_roles
                     WHERE rolname IN ('zns_app','zns_fake','zns_registration_operator',
                                       'zns_meter','zns_inventory')
                     AND (rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls))
);
ROLLBACK;
