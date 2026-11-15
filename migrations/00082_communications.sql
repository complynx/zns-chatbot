-- +goose Up
CREATE TABLE shuttle_communications (
    id uuid PRIMARY KEY,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    author_id uuid NOT NULL REFERENCES shuttle_users(id),
    operation_id uuid NOT NULL,
    event_id uuid REFERENCES shuttle_events(id),
    arguments jsonb NOT NULL,
    state text NOT NULL DEFAULT 'prepared' CHECK (state IN ('prepared','queued','cancelled')),
    category text NOT NULL CHECK (category IN ('personal','advertising','information','warning')),
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expires_at timestamptz NOT NULL DEFAULT clock_timestamp() + interval '30 minutes',
    UNIQUE(bot_id,organization_id,author_id,operation_id)
);
CREATE TABLE shuttle_communication_deliveries (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    communication_id uuid NOT NULL REFERENCES shuttle_communications(id),
    recipient_id uuid NOT NULL REFERENCES shuttle_users(id),
    kind text NOT NULL CHECK (kind IN ('confirmation','message')),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN
      ('pending','retryable','sending','delivered','delivery_unknown','delivery_failed','delivery_blocked','skipped')),
    version bigint NOT NULL DEFAULT 1,
    attempt_id uuid,
    rendered_text text,
    personalization_run_id text,
    personalization_chat_id text,
    personalization_input jsonb,
    personalization_started_at timestamptz,
    personalization_polls integer NOT NULL DEFAULT 0 CHECK (personalization_polls >= 0),
    receipt jsonb NOT NULL DEFAULT '{}',
    next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE(communication_id,recipient_id,kind)
);
CREATE INDEX shuttle_communication_delivery_ready ON shuttle_communication_deliveries(id)
 WHERE state IN ('pending','retryable','sending');
CREATE INDEX shuttle_communication_delivery_reply ON shuttle_communication_deliveries(recipient_id,(receipt->>'message_id'))
 WHERE kind='message' AND state='delivered';
CREATE INDEX shuttle_communication_personalizing ON shuttle_communication_deliveries(communication_id)
 WHERE personalization_started_at IS NOT NULL AND rendered_text IS NULL AND state IN ('pending','retryable');
CREATE TABLE shuttle_broadcast_subscriptions (
    user_id uuid NOT NULL REFERENCES shuttle_users(id),
    scope text NOT NULL CHECK (scope IN ('all','general') OR scope ~ '^[0-9a-f-]{36}$'),
    category text NOT NULL CHECK (category IN ('advertising','information')),
    subscribed boolean NOT NULL,
    changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY(user_id,scope,category)
);

-- +goose Down
-- Preserve queued messages and people's subscription choices on rollback.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shuttle_communications) OR EXISTS (SELECT 1 FROM shuttle_broadcast_subscriptions) THEN
        RAISE EXCEPTION 'Communication history and subscriptions require a reviewed forward migration';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_broadcast_subscriptions;
DROP TABLE shuttle_communication_deliveries;
DROP TABLE shuttle_communications;
