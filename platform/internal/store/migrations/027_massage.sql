CREATE TABLE core.massage_events (
  id text PRIMARY KEY, daily_limit integer NOT NULL DEFAULT 3 CHECK(daily_limit BETWEEN 1 AND 100),
  prior_long interval NOT NULL DEFAULT interval '1 hour' CHECK(prior_long > interval '0'),
  prior_short interval NOT NULL DEFAULT interval '10 minutes' CHECK(prior_short > interval '0')
);
CREATE TABLE core.massage_parties (
  id text PRIMARY KEY, event_id text NOT NULL REFERENCES core.massage_events(id),
  starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL,
  tables integer NOT NULL CHECK(tables BETWEEN 0 AND 100), position integer NOT NULL DEFAULT 0,
  is_open boolean NOT NULL DEFAULT false,
  CHECK(ends_at > starts_at AND ends_at <= starts_at + interval '2 days'), UNIQUE(event_id,id)
);
CREATE TABLE core.massage_specialists (
  event_id text NOT NULL REFERENCES core.massage_events(id), owner text NOT NULL REFERENCES core.users(id),
  name text NOT NULL, icon text NOT NULL DEFAULT '💆', about jsonb NOT NULL DEFAULT '{}',
  min_length integer NOT NULL DEFAULT 1 CHECK(min_length BETWEEN 1 AND 6),
  max_length integer NOT NULL DEFAULT 1000 CHECK(max_length >= min_length AND max_length <= 1000),
  legacy_table_flag boolean NOT NULL DEFAULT false,
  notify_bookings boolean NOT NULL DEFAULT true, notify_next boolean NOT NULL DEFAULT true,
  PRIMARY KEY(event_id,owner)
);
CREATE TABLE core.massage_work (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, event_id text NOT NULL, specialist text NOT NULL,
  starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL,
  FOREIGN KEY(event_id,specialist) REFERENCES core.massage_specialists(event_id,owner),
  CHECK(ends_at > starts_at AND ends_at <= starts_at + interval '2 days')
);
CREATE TABLE core.massage_bookings (
  id text PRIMARY KEY, event_id text NOT NULL, party_id text NOT NULL,
  owner text NOT NULL REFERENCES core.users(id), specialist text NOT NULL,
  slot integer NOT NULL CHECK(slot BETWEEN -144 AND 288), length integer NOT NULL CHECK(length BETWEEN 1 AND 6),
  starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL,
  price integer NOT NULL CHECK(price > 0), price_rub integer NOT NULL CHECK(price_rub > 0),
  version bigint NOT NULL DEFAULT 1 CHECK(version > 0), cancelled_at timestamptz,
  instant boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(event_id,party_id) REFERENCES core.massage_parties(event_id,id),
  FOREIGN KEY(event_id,specialist) REFERENCES core.massage_specialists(event_id,owner)
);
CREATE INDEX massage_booking_party ON core.massage_bookings(event_id,party_id) WHERE cancelled_at IS NULL;
CREATE INDEX massage_booking_owner ON core.massage_bookings(owner,event_id,starts_at);
CREATE TABLE core.massage_operations (
  actor text NOT NULL REFERENCES core.users(id), key text NOT NULL,
  request_hash text NOT NULL, result jsonb NOT NULL, PRIMARY KEY(actor,key)
);
CREATE TABLE core.massage_notices (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  booking_id text NOT NULL REFERENCES core.massage_bookings(id), owner text NOT NULL REFERENCES core.users(id),
  kind text NOT NULL CHECK(kind IN ('booked','cancelled','prior_long','prior_short','next','additional')),
  created_at timestamptz NOT NULL DEFAULT now(), sent_at timestamptz,
  UNIQUE(booking_id,owner,kind)
);
