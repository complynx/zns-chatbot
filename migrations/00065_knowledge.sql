-- +goose Up
CREATE UNIQUE INDEX shuttle_events_knowledge_scope ON shuttle_events(bot_id,organization_id,id);

CREATE TABLE shuttle_knowledge_articles (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(char_length(organization_id) BETWEEN 1 AND 256),
 event_id uuid,
 slug text NOT NULL CHECK(char_length(slug) BETWEEN 1 AND 100),
 title text NOT NULL CHECK(char_length(title) BETWEEN 1 AND 200),
 language text NOT NULL CHECK(language IN ('en','ru')),
 topics text[] NOT NULL DEFAULT '{}' CHECK(cardinality(topics)<=12 AND array_position(topics,NULL) IS NULL),
 body text NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 98304),
 sources jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(sources)='array' AND jsonb_array_length(sources)<=16 AND octet_length(sources::text)<=131072),
 current_revision bigint NOT NULL CHECK(current_revision>0),
 event_name text CHECK(char_length(event_name) BETWEEN 1 AND 256),
 event_starts_at timestamptz,
 event_ends_at timestamptz,
 event_timezone text CHECK(char_length(event_timezone) BETWEEN 1 AND 128),
 archived_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 search_vector tsvector GENERATED ALWAYS AS (
  setweight(to_tsvector('english'::regconfig,title),'A') ||
  setweight(to_tsvector('russian'::regconfig,title),'A') ||
  setweight(to_tsvector('simple'::regconfig,title),'A') ||
  setweight(to_tsvector('english'::regconfig,body),'B') ||
  setweight(to_tsvector('russian'::regconfig,body),'B') ||
  setweight(to_tsvector('simple'::regconfig,body),'B')
 ) STORED,
 UNIQUE(bot_id,organization_id,id),
 FOREIGN KEY(bot_id,organization_id,event_id) REFERENCES shuttle_events(bot_id,organization_id,id),
 CHECK((event_id IS NULL AND event_name IS NULL AND event_starts_at IS NULL AND event_ends_at IS NULL AND event_timezone IS NULL AND archived_at IS NULL) OR
  (event_id IS NOT NULL AND event_name IS NOT NULL AND event_starts_at IS NOT NULL AND event_ends_at IS NOT NULL AND event_timezone IS NOT NULL AND event_ends_at>event_starts_at)),
 CHECK(archived_at IS NULL OR archived_at>=event_ends_at)
);
-- Origin identity survives archival; NULL origins have their own uniqueness rule.
CREATE UNIQUE INDEX shuttle_knowledge_articles_general_slug ON shuttle_knowledge_articles(bot_id,organization_id,language,slug) WHERE event_id IS NULL;
CREATE UNIQUE INDEX shuttle_knowledge_articles_event_slug ON shuttle_knowledge_articles(bot_id,organization_id,event_id,language,slug) WHERE event_id IS NOT NULL;
CREATE INDEX shuttle_knowledge_articles_search ON shuttle_knowledge_articles USING gin(search_vector);
CREATE INDEX shuttle_knowledge_articles_topics ON shuttle_knowledge_articles USING gin(topics);
CREATE INDEX shuttle_knowledge_articles_browse ON shuttle_knowledge_articles(bot_id,organization_id,event_id,language,id);
CREATE INDEX shuttle_knowledge_articles_archive ON shuttle_knowledge_articles(event_id,id) WHERE event_id IS NOT NULL AND archived_at IS NULL;

CREATE TABLE shuttle_knowledge_revisions (
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(char_length(organization_id) BETWEEN 1 AND 256),
 article_id uuid NOT NULL,
 revision bigint NOT NULL CHECK(revision>0),
 previous_revision bigint,
 change_kind text NOT NULL CHECK(change_kind IN ('create','edit','approved_suggestion','archive')),
 author_id uuid REFERENCES shuttle_users(id),
 proposer_id uuid REFERENCES shuttle_users(id),
 reviewer_id uuid REFERENCES shuttle_users(id),
 suggestion_id uuid,
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 2000),
 slug text NOT NULL CHECK(char_length(slug) BETWEEN 1 AND 100),
 title text NOT NULL CHECK(char_length(title) BETWEEN 1 AND 200),
 language text NOT NULL CHECK(language IN ('en','ru')),
 topics text[] NOT NULL DEFAULT '{}' CHECK(cardinality(topics)<=12 AND array_position(topics,NULL) IS NULL),
 body text NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 98304),
 sources jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(sources)='array' AND jsonb_array_length(sources)<=16 AND octet_length(sources::text)<=131072),
 event_id uuid,
 event_name text CHECK(char_length(event_name) BETWEEN 1 AND 256),
 event_starts_at timestamptz,
 event_ends_at timestamptz,
 event_timezone text CHECK(char_length(event_timezone) BETWEEN 1 AND 128),
 archived_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(bot_id,organization_id,article_id,revision),
 FOREIGN KEY(bot_id,organization_id,article_id) REFERENCES shuttle_knowledge_articles(bot_id,organization_id,id),
 FOREIGN KEY(bot_id,organization_id,article_id,previous_revision) REFERENCES shuttle_knowledge_revisions(bot_id,organization_id,article_id,revision),
 FOREIGN KEY(bot_id,organization_id,event_id) REFERENCES shuttle_events(bot_id,organization_id,id),
 CHECK((revision=1 AND previous_revision IS NULL) OR (revision>1 AND previous_revision IS NOT NULL AND previous_revision=revision-1)),
 CHECK((event_id IS NULL AND event_name IS NULL AND event_starts_at IS NULL AND event_ends_at IS NULL AND event_timezone IS NULL AND archived_at IS NULL) OR
  (event_id IS NOT NULL AND event_name IS NOT NULL AND event_starts_at IS NOT NULL AND event_ends_at IS NOT NULL AND event_timezone IS NOT NULL AND event_ends_at>event_starts_at)),
 CHECK(archived_at IS NULL OR archived_at>=event_ends_at),
 CHECK((change_kind='archive' AND author_id IS NULL AND event_id IS NOT NULL AND archived_at IS NOT NULL) OR (change_kind<>'archive' AND author_id IS NOT NULL)),
 CHECK((change_kind='approved_suggestion' AND proposer_id IS NOT NULL AND reviewer_id IS NOT NULL AND proposer_id<>reviewer_id AND author_id=reviewer_id AND suggestion_id IS NOT NULL) OR
  (change_kind<>'approved_suggestion' AND proposer_id IS NULL AND reviewer_id IS NULL AND suggestion_id IS NULL))
);
ALTER TABLE shuttle_knowledge_articles ADD CONSTRAINT shuttle_knowledge_articles_current_revision
 FOREIGN KEY(bot_id,organization_id,id,current_revision) REFERENCES shuttle_knowledge_revisions(bot_id,organization_id,article_id,revision) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE shuttle_knowledge_suggestions (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(char_length(organization_id) BETWEEN 1 AND 256),
 -- A new article target does not exist until approval; deliberately no FK.
 article_id uuid NOT NULL,
 base_revision bigint NOT NULL CHECK(base_revision>=0),
 event_id uuid,
 event_version bigint CHECK(event_version>0),
 proposer_id uuid NOT NULL REFERENCES shuttle_users(id),
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 2000),
 slug text NOT NULL CHECK(char_length(slug) BETWEEN 1 AND 100),
 title text NOT NULL CHECK(char_length(title) BETWEEN 1 AND 200),
 language text NOT NULL CHECK(language IN ('en','ru')),
 topics text[] NOT NULL DEFAULT '{}' CHECK(cardinality(topics)<=12 AND array_position(topics,NULL) IS NULL),
 body text NOT NULL CHECK(octet_length(body) BETWEEN 1 AND 98304),
 sources jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(sources)='array' AND jsonb_array_length(sources)<=16 AND octet_length(sources::text)<=131072),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','approved','rejected')),
 reviewer_id uuid REFERENCES shuttle_users(id),
 decision_reason text CHECK(char_length(decision_reason) BETWEEN 1 AND 2000),
 decided_at timestamptz,
 published_revision bigint CHECK(published_revision>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(bot_id,organization_id,id),
 FOREIGN KEY(bot_id,organization_id,event_id) REFERENCES shuttle_events(bot_id,organization_id,id),
 FOREIGN KEY(bot_id,organization_id,article_id,published_revision) REFERENCES shuttle_knowledge_revisions(bot_id,organization_id,article_id,revision),
 CHECK((event_id IS NULL)=(event_version IS NULL)),
 CHECK(reviewer_id IS NULL OR reviewer_id<>proposer_id),
 CHECK((state='pending' AND reviewer_id IS NULL AND decided_at IS NULL AND decision_reason IS NULL AND published_revision IS NULL) OR
  (state='approved' AND reviewer_id IS NOT NULL AND decided_at IS NOT NULL AND decision_reason IS NOT NULL AND published_revision IS NOT NULL) OR
  (state='rejected' AND reviewer_id IS NOT NULL AND decided_at IS NOT NULL AND decision_reason IS NOT NULL AND published_revision IS NULL)),
 CHECK(decided_at IS NULL OR decided_at>=created_at)
);
ALTER TABLE shuttle_knowledge_revisions ADD CONSTRAINT shuttle_knowledge_revisions_suggestion
 FOREIGN KEY(bot_id,organization_id,suggestion_id) REFERENCES shuttle_knowledge_suggestions(bot_id,organization_id,id);
CREATE UNIQUE INDEX shuttle_knowledge_revisions_archive_once ON shuttle_knowledge_revisions(bot_id,organization_id,article_id) WHERE change_kind='archive';
CREATE UNIQUE INDEX shuttle_knowledge_revisions_suggestion_once ON shuttle_knowledge_revisions(bot_id,organization_id,suggestion_id) WHERE suggestion_id IS NOT NULL;
CREATE INDEX shuttle_knowledge_suggestions_pending ON shuttle_knowledge_suggestions(bot_id,organization_id,event_id,created_at,id) WHERE state='pending';
CREATE INDEX shuttle_knowledge_suggestions_owner ON shuttle_knowledge_suggestions(bot_id,organization_id,proposer_id,created_at DESC,id);

CREATE TABLE shuttle_knowledge_moderators (
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(char_length(organization_id) BETWEEN 1 AND 256),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 enabled boolean NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 changed_by uuid NOT NULL REFERENCES shuttle_users(id),
 changed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(bot_id,organization_id,user_id)
);

-- Exact consent and receipts share one knowledge-specific vocabulary.
-- Preparations may expire; suggestion decisions and operation receipts persist.
CREATE TABLE shuttle_knowledge_preparations (
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(char_length(organization_id) BETWEEN 1 AND 256),
 actor text NOT NULL CHECK(char_length(actor) BETWEEN 1 AND 256),
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 action_kind text NOT NULL CHECK(action_kind IN ('suggest','edit','review','set_moderator')),
 operation_id text NOT NULL CHECK(char_length(operation_id) BETWEEN 1 AND 128),
 proposal_id uuid NOT NULL UNIQUE,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object' AND octet_length(document::text)<=1048576),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL,
 PRIMARY KEY(bot_id,organization_id,actor,action_kind,operation_id),
 CHECK(expires_at>created_at)
);
CREATE TABLE shuttle_knowledge_operations (
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(char_length(organization_id) BETWEEN 1 AND 256),
 actor text NOT NULL CHECK(char_length(actor) BETWEEN 1 AND 256),
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 action_kind text NOT NULL CHECK(action_kind IN ('suggest','edit','review','set_moderator')),
 operation_id text NOT NULL CHECK(char_length(operation_id) BETWEEN 1 AND 128),
 proposal_id uuid NOT NULL UNIQUE,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=2097152),
 completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(bot_id,organization_id,actor,action_kind,operation_id)
);

-- +goose Down
-- Binary rollback retains these tables. Intentional schema rollback requires
-- explicit export/removal; preparations alone also prevent accidental loss.
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_knowledge_operations,shuttle_knowledge_preparations,
  shuttle_knowledge_moderators,shuttle_knowledge_suggestions,
  shuttle_knowledge_revisions,shuttle_knowledge_articles IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_knowledge_articles)
  OR EXISTS(SELECT 1 FROM shuttle_knowledge_revisions)
  OR EXISTS(SELECT 1 FROM shuttle_knowledge_suggestions)
  OR EXISTS(SELECT 1 FROM shuttle_knowledge_moderators)
  OR EXISTS(SELECT 1 FROM shuttle_knowledge_preparations)
  OR EXISTS(SELECT 1 FROM shuttle_knowledge_operations) THEN
  RAISE EXCEPTION 'Export and deliberately remove knowledge articles, revisions, suggestions, moderators and approvals before schema rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_knowledge_articles DROP CONSTRAINT shuttle_knowledge_articles_current_revision;
ALTER TABLE shuttle_knowledge_revisions DROP CONSTRAINT shuttle_knowledge_revisions_suggestion;
DROP TABLE shuttle_knowledge_operations;
DROP TABLE shuttle_knowledge_preparations;
DROP TABLE shuttle_knowledge_moderators;
DROP TABLE shuttle_knowledge_suggestions;
DROP TABLE shuttle_knowledge_revisions;
DROP TABLE shuttle_knowledge_articles;
DROP INDEX shuttle_events_knowledge_scope;
