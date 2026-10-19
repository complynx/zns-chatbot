-- +goose Up
CREATE TABLE shuttle_payment_evidence (
 id uuid PRIMARY KEY, event_id uuid NOT NULL REFERENCES shuttle_events(id),
 pass_id uuid NOT NULL REFERENCES shuttle_passes(id), user_id uuid NOT NULL REFERENCES shuttle_users(id),
 file_id uuid NOT NULL REFERENCES shuttle_files(id), source_unique_id text NOT NULL,
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 9007199254740991),
 currency text NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), currency_scale integer NOT NULL CHECK(currency_scale BETWEEN 0 AND 4),
 purpose text NOT NULL CHECK(purpose IN ('initial','top_up')),
 sent_at timestamptz NOT NULL, submitted_at timestamptz NOT NULL DEFAULT clock_timestamp(), due_at timestamptz NOT NULL
);
CREATE INDEX shuttle_payment_evidence_source ON shuttle_payment_evidence(user_id,source_unique_id);
CREATE INDEX shuttle_payment_evidence_event ON shuttle_payment_evidence(event_id,id);
CREATE TABLE shuttle_payment_versions (
 pass_id uuid PRIMARY KEY REFERENCES shuttle_passes(id), version bigint NOT NULL CHECK(version>0)
);
CREATE TABLE shuttle_payment_reviews (
 id uuid PRIMARY KEY, evidence_id uuid NOT NULL UNIQUE REFERENCES shuttle_payment_evidence(id),
 actor_id uuid NOT NULL REFERENCES shuttle_users(id), actor_subject text NOT NULL,
 authority_role text NOT NULL CHECK(authority_role IN ('global_administrator','ambassador','administrator')),
 decision text NOT NULL CHECK(decision IN ('confirmed','rejected')),
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 0 AND 9007199254740991),
 reason text NOT NULL CHECK(char_length(reason)<=512), reviewed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((decision='confirmed' AND amount_minor>0) OR (decision='rejected' AND amount_minor=0 AND char_length(reason)>0))
);
CREATE TABLE shuttle_payment_operations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, kind text NOT NULL CHECK(kind IN ('submit','review')),
 request_hash bytea NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
CREATE TABLE shuttle_payment_preparations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, proposal_id uuid NOT NULL UNIQUE, request_hash bytea NOT NULL,
 expires_at timestamptz NOT NULL, document jsonb NOT NULL,
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
-- +goose Down
-- Export submitted evidence, decisions and the confirmed ledger before rollback.
DROP TABLE shuttle_payment_preparations;
DROP TABLE shuttle_payment_operations;
DROP TABLE shuttle_payment_reviews;
DROP TABLE shuttle_payment_versions;
DROP TABLE shuttle_payment_evidence;
