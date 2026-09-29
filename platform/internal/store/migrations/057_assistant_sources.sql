-- Source-owned records do not share mutation keys with curated/private memory.
CREATE TABLE core.assistant_sources (
 slot text PRIMARY KEY CHECK(slot IN ('assistant_qa','assistant_about')),
 identity text NOT NULL, version bigint NOT NULL DEFAULT 0,
 digest text NOT NULL DEFAULT '', status text NOT NULL DEFAULT 'unavailable',
 attempted_at timestamptz, refreshed_at timestamptz,
 error_code text NOT NULL DEFAULT ''
);
CREATE TABLE core.assistant_source_documents (
 slot text NOT NULL REFERENCES core.assistant_sources(slot), identity text NOT NULL,
 item_key text NOT NULL, body text NOT NULL CHECK(octet_length(body)<=64000),
 version bigint NOT NULL CHECK(version>0), PRIMARY KEY(slot,identity,item_key)
);
CREATE TABLE core.assistant_source_versions (
 slot text NOT NULL REFERENCES core.assistant_sources(slot), identity text NOT NULL,
 version bigint NOT NULL, source_digest text NOT NULL, text_digest text NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(slot,identity,version)
);
