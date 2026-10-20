-- +goose Up
-- Suggestions are the durable source. Maintenance fills missing recipient rows.
CREATE TABLE shuttle_knowledge_notices (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 bot_id bigint NOT NULL CHECK(bot_id>0), organization_id text NOT NULL,
 suggestion_id uuid NOT NULL, recipient_id uuid NOT NULL REFERENCES shuttle_users(id),
 kind text NOT NULL CHECK(kind IN ('pending','approved','rejected')),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN
 ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0), attempt_id uuid, payload jsonb,
 receipt jsonb NOT NULL DEFAULT '{}',
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(bot_id,organization_id,suggestion_id,recipient_id,kind),
 FOREIGN KEY(bot_id,organization_id,suggestion_id)
 REFERENCES shuttle_knowledge_suggestions(bot_id,organization_id,id),
 CHECK(state<>'send_inflight' OR (attempt_id IS NOT NULL AND payload IS NOT NULL))
);
CREATE INDEX shuttle_knowledge_notices_ready ON shuttle_knowledge_notices(bot_id,organization_id,id)
 WHERE state IN ('pending','retryable');
CREATE INDEX shuttle_knowledge_notices_recipient ON shuttle_knowledge_notices(bot_id,organization_id,recipient_id,id);

-- No titles or proposed content enter delivery discovery. A global IAM role is
-- not a subscription: reviewers are explicitly named moderators/event admins.
CREATE VIEW shuttle_knowledge_notice_candidates AS
SELECT s.bot_id,s.organization_id,s.id AS suggestion_id,u.id AS recipient_id,s.state AS kind
FROM shuttle_knowledge_suggestions s
LEFT JOIN shuttle_events e ON e.id=s.event_id AND e.bot_id=s.bot_id AND e.organization_id=s.organization_id
JOIN shuttle_users u ON u.bot_id=s.bot_id AND u.zitadel_org=s.organization_id
AND NOT u.revoked AND u.zitadel_subject<>''
WHERE (s.state IN ('approved','rejected') AND u.id=s.proposer_id)
OR (s.state='pending' AND u.id<>s.proposer_id AND (
 ((s.event_id IS NULL OR e.ends_at<=clock_timestamp() OR EXISTS(
  SELECT 1 FROM shuttle_knowledge_articles h WHERE h.bot_id=s.bot_id AND h.organization_id=s.organization_id
  AND h.event_id=s.event_id AND h.archived_at IS NOT NULL))
 AND EXISTS(SELECT 1 FROM shuttle_knowledge_moderators m WHERE m.bot_id=s.bot_id
  AND m.organization_id=s.organization_id AND m.user_id=u.id AND m.enabled))
 OR (s.event_id IS NOT NULL AND e.ends_at>clock_timestamp() AND NOT EXISTS(
  SELECT 1 FROM shuttle_knowledge_articles h WHERE h.bot_id=s.bot_id AND h.organization_id=s.organization_id
  AND h.event_id=s.event_id AND h.archived_at IS NOT NULL)
 AND EXISTS(SELECT 1 FROM shuttle_event_authorities a WHERE a.event_id=s.event_id
  AND a.user_id=u.id AND a.role='administrator'))));

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_knowledge_notices IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_knowledge_notices) THEN
  RAISE EXCEPTION 'Export knowledge notice delivery history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP VIEW shuttle_knowledge_notice_candidates;
DROP TABLE shuttle_knowledge_notices;
