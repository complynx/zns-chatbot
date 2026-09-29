-- Transitional common event metadata adapter: pass_events remains the sole
-- authority for the end time. An order sales deadline is not an event end.
CREATE VIEW core.events AS SELECT id, finishes_at FROM core.pass_events;
CREATE TABLE core.knowledge_scopes (
 scope text PRIMARY KEY, event_id text UNIQUE REFERENCES core.pass_events(id),
 CHECK(scope=coalesce(event_id,''))
);
INSERT INTO core.knowledge_scopes(scope) VALUES('');
CREATE TABLE core.knowledge_permissions (
 scope text NOT NULL REFERENCES core.knowledge_scopes(scope),
 actor text NOT NULL REFERENCES core.users(id),
 permission text NOT NULL CHECK(permission IN ('curate','review')),
 PRIMARY KEY(scope,actor,permission)
);
CREATE TABLE core.knowledge_facts (
 scope text NOT NULL REFERENCES core.knowledge_scopes(scope), topic text NOT NULL, fact_key text NOT NULL,
 body text NOT NULL CHECK(octet_length(body)<=8000), version bigint NOT NULL CHECK(version>0),
 active boolean NOT NULL DEFAULT true, updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(scope,topic,fact_key)
);
CREATE TABLE core.knowledge_proposals (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 scope text NOT NULL REFERENCES core.knowledge_scopes(scope), owner text NOT NULL REFERENCES core.users(id),
 topic text NOT NULL, fact_key text NOT NULL, body text NOT NULL CHECK(octet_length(body)<=8000),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), fact_version bigint NOT NULL CHECK(fact_version>=0),
 state text NOT NULL CHECK(state IN ('pending_filter','pending_review','filtered','approved','rejected')),
 reason text NOT NULL DEFAULT '' CHECK(octet_length(reason)<=2048),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX knowledge_proposals_owner ON core.knowledge_proposals(owner,id);
CREATE INDEX knowledge_proposals_review ON core.knowledge_proposals(scope,state,id);
CREATE TABLE core.knowledge_memos (
 owner text NOT NULL REFERENCES core.users(id), memo_key text NOT NULL,
 body text NOT NULL CHECK(octet_length(body)<=2048), version bigint NOT NULL CHECK(version>0),
 active boolean NOT NULL DEFAULT true, PRIMARY KEY(owner,memo_key)
);
CREATE TABLE core.knowledge_operations (
 actor text NOT NULL REFERENCES core.users(id), key_hash text NOT NULL, request_hash text NOT NULL,
 result jsonb NOT NULL, PRIMARY KEY(actor,key_hash)
);
CREATE TABLE core.knowledge_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, actor text NOT NULL REFERENCES core.users(id),
 scope text NOT NULL, action text NOT NULL, subject text NOT NULL, version bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
