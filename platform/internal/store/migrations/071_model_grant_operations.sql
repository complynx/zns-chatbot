CREATE TABLE core.model_grant_operations (
    actor text NOT NULL REFERENCES core.users(id),
    operation_key text NOT NULL CHECK (length(operation_key) BETWEEN 1 AND 128),
    request jsonb NOT NULL,
    result jsonb NOT NULL,
    PRIMARY KEY (actor, operation_key)
);

DO $$
DECLARE runtime_role text;
BEGIN
    FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app', 'zns_api', 'zns_runtime') LOOP
        EXECUTE format('GRANT SELECT,INSERT ON core.model_grant_operations TO %I', runtime_role);
    END LOOP;
END $$;
