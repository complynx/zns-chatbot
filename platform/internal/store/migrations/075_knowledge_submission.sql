ALTER TABLE core.knowledge_proposals DROP CONSTRAINT knowledge_proposals_state_check;
ALTER TABLE core.knowledge_proposals ADD CHECK(state IN
 ('pending_filter','awaiting_submission','pending_review','filtered','approved','rejected'));
-- Unreleased proposals have no author consent. They must return to the author.
UPDATE core.knowledge_proposals SET state='awaiting_submission' WHERE state='pending_review';
CREATE TABLE core.knowledge_proposal_submissions (
 proposal_id bigint PRIMARY KEY REFERENCES core.knowledge_proposals(id),
 owner text NOT NULL REFERENCES core.users(id),
 proposal_version bigint NOT NULL CHECK(proposal_version>0),
 scope text NOT NULL, topic text NOT NULL, fact_key text NOT NULL,
 body_sha256 text NOT NULL CHECK(body_sha256 ~ '^[0-9a-f]{64}$'),
 submitted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
