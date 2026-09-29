CREATE SCHEMA IF NOT EXISTS credits;
CREATE TABLE credits.attempts (
 id uuid PRIMARY KEY,
 operation_key text NOT NULL,
 actor text NOT NULL,
 payer text NOT NULL,
 operation text NOT NULL,
 provider text NOT NULL,
 model text NOT NULL,
 state text NOT NULL CHECK(state IN ('reserved','dispatched','settled','not_sent')),
 reserved_nano_usd bigint CHECK(reserved_nano_usd>=0),
 usage jsonb,
 usage_sha256 text,
 cost_nano_usd bigint CHECK(cost_nano_usd>=0),
 cost_basis text NOT NULL DEFAULT 'unknown' CHECK(cost_basis IN ('unknown','estimated','provider_reported','known_free')),
 price_version text,
 conversion_version text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 dispatched_at timestamptz,
 settled_at timestamptz
);
CREATE INDEX credit_attempt_payer_time ON credits.attempts(payer,created_at,id);
CREATE INDEX credit_attempt_operation ON credits.attempts(operation_key,id);
CREATE TABLE credits.price_versions (
 version text PRIMARY KEY,
 provider text NOT NULL,
 model text NOT NULL,
 specification jsonb NOT NULL,
 source text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE credits.price_selection (
 provider text NOT NULL,
 model text NOT NULL,
 version text NOT NULL REFERENCES credits.price_versions(version),
 PRIMARY KEY(provider,model)
);
