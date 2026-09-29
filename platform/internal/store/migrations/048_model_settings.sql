CREATE TABLE core.model_setting_grants (
 owner text NOT NULL REFERENCES core.users(id),
 capability text NOT NULL CHECK (capability IN ('own','others','global')),
 PRIMARY KEY(owner,capability)
);
CREATE TABLE core.model_settings (
 scope text PRIMARY KEY,
 model text NOT NULL DEFAULT '',
 effort text NOT NULL DEFAULT '',
 version bigint NOT NULL DEFAULT 0,
 author text NOT NULL,
 authority text NOT NULL CHECK(authority IN ('own','others','global'))
);
CREATE TABLE core.model_setting_operations (
 actor text NOT NULL,
 operation_key text NOT NULL,
 request jsonb NOT NULL,
 result jsonb NOT NULL,
 PRIMARY KEY(actor,operation_key)
);
