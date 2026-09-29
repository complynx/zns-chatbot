CREATE TABLE core.registration_ingress (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('telegram','application')),
    bot_id bigint NOT NULL,
    request_key text NOT NULL,
    owner text NOT NULL DEFAULT '',
    telegram_id bigint NOT NULL DEFAULT 0,
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(kind,bot_id,request_key,owner)
);

CREATE TABLE core.registration_intents (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_id text NOT NULL REFERENCES core.pass_events(id),
    owner text NOT NULL REFERENCES core.users(id),
    generation bigint NOT NULL,
    ingress_id bigint REFERENCES core.registration_ingress(id),
    origin text NOT NULL CHECK (origin IN ('canonical_ingress','legacy_fallback')),
    state text NOT NULL CHECK (state IN ('captured','registered','cancelled')),
    sales_open boolean,
    checked_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    booking_created_at timestamptz,
    terminal_reason text NOT NULL DEFAULT '',
    closed_through_position bigint NOT NULL DEFAULT 0,
    UNIQUE(event_id,owner,generation)
);
CREATE UNIQUE INDEX registration_intents_active ON core.registration_intents(event_id,owner)
WHERE state <> 'cancelled';

CREATE TABLE core.registration_intent_requests (
    event_id text NOT NULL,
    owner text NOT NULL,
    key_hash text NOT NULL,
    request_hash text NOT NULL,
    intent_id bigint NOT NULL REFERENCES core.registration_intents(id),
    PRIMARY KEY(event_id,owner,key_hash)
);

-- Historical timestamps remain generation evidence, never fabricated ingress.
INSERT INTO core.registration_intents(event_id,owner,generation,origin,state,sales_open,booking_created_at,terminal_reason)
SELECT event_id,owner,1,'legacy_fallback',CASE WHEN state='cancelled' THEN 'cancelled' ELSE 'registered' END,
 NULL,created_at,CASE WHEN state='cancelled' THEN 'legacy_cancelled' ELSE '' END
FROM core.pass_bookings;

DO $$
DECLARE runtime_role text;
BEGIN
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_api','zns_runtime') LOOP
  EXECUTE format('GRANT SELECT,INSERT,UPDATE,DELETE ON core.registration_ingress,core.registration_intents,core.registration_intent_requests TO %I',runtime_role);
  EXECUTE format('GRANT USAGE,SELECT ON SEQUENCE core.registration_ingress_id_seq,core.registration_intents_id_seq TO %I',runtime_role);
 END LOOP;
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_bot') LOOP
  EXECUTE format('GRANT SELECT,INSERT ON core.registration_ingress TO %I',runtime_role);
  EXECUTE format('GRANT USAGE,SELECT ON SEQUENCE core.registration_ingress_id_seq TO %I',runtime_role);
 END LOOP;
END $$;