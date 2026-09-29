-- One database is one bot deployment, as with core.users and core.pass_profiles.
CREATE TABLE core.pass_events (
 id text PRIMARY KEY, finishes_at timestamptz NOT NULL,
 passport_required boolean NOT NULL DEFAULT false,
 assignment_rule text NOT NULL DEFAULT 'distributed' CHECK (assignment_rule IN ('paired','distributed')),
 disable_concurrency_limit boolean NOT NULL DEFAULT false
);
CREATE TABLE core.pass_event_tiers (
 event_id text NOT NULL REFERENCES core.pass_events(id), position integer NOT NULL CHECK(position>=0),
 amount integer NOT NULL CHECK(amount BETWEEN 0 AND 1000000),
 price integer NOT NULL CHECK(price BETWEEN 1 AND 1000000000), starts_at timestamptz NOT NULL,
 promo boolean NOT NULL DEFAULT false, blocked_by_date boolean NOT NULL DEFAULT false,
 PRIMARY KEY(event_id,position)
);
CREATE TABLE core.pass_booking_admins (owner text PRIMARY KEY REFERENCES core.users(id));
CREATE TABLE core.pass_payment_admins (
 event_id text NOT NULL REFERENCES core.pass_events(id), owner text NOT NULL REFERENCES core.users(id),
 hidden boolean NOT NULL DEFAULT false, PRIMARY KEY(event_id,owner)
);
CREATE TABLE core.pass_bookings (
 event_id text NOT NULL REFERENCES core.pass_events(id), owner text NOT NULL REFERENCES core.users(id),
 version bigint NOT NULL CHECK(version>0),
 state text NOT NULL CHECK(state IN ('waiting-for-couple','waitlist','assigned','paid','cancelled')),
 role text NOT NULL CHECK(role IN ('leader','follower')),
 kind text NOT NULL CHECK(kind IN ('solo','couple')),
 partner text NOT NULL DEFAULT '', invitation_target bigint NOT NULL DEFAULT 0 CHECK(invitation_target>=0),
 payment_admin text NOT NULL REFERENCES core.users(id), created_at timestamptz NOT NULL,
 assigned_at timestamptz, price integer CHECK(price>=0), tier_index integer CHECK(tier_index>=0),
 skip_balance boolean,
 PRIMARY KEY(event_id,owner),
 CHECK(partner<>owner),
 CHECK((state='waiting-for-couple')=(invitation_target>0)),
 CHECK((partner<>'')=(kind='couple' AND state IN ('waitlist','assigned','paid'))),
 CHECK((state IN ('assigned','paid'))=(assigned_at IS NOT NULL AND price IS NOT NULL))
);
CREATE TABLE core.pass_booking_operations (
 event_id text NOT NULL REFERENCES core.pass_events(id), actor text NOT NULL REFERENCES core.users(id),
 key_hash text NOT NULL, request_hash text NOT NULL, PRIMARY KEY(event_id,actor,key_hash)
);
