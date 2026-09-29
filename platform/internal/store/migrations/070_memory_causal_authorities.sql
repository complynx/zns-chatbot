-- Only host-derived memory revisions receive causal metadata. Python originals
-- remain original, without invented sources. Approved derived publications retain
-- causal metadata; only their explicit publication scope changes reader access.
ALTER TABLE core.memory_revisions ADD COLUMN origin text NOT NULL DEFAULT 'original' CHECK(origin IN ('original','derived'));
CREATE TABLE core.memory_read_authorities (
 namespace text NOT NULL, owner text NOT NULL, scope text NOT NULL,
 topic text NOT NULL, item_key text NOT NULL, source_kind text NOT NULL, version bigint NOT NULL,
 authorities jsonb NOT NULL CHECK(jsonb_typeof(authorities)='array' AND octet_length(authorities::text)<=1048576),
 revoked boolean NOT NULL DEFAULT false,
 PRIMARY KEY(namespace,owner,scope,topic,item_key,source_kind,version),
 FOREIGN KEY(namespace,owner,scope,topic,item_key,source_kind,version)
 REFERENCES core.memory_revisions(namespace,owner,scope,topic,item_key,source_kind,version)
);

-- Proposal bodies are immutable; decisions advance state/version, not content.
ALTER TABLE core.knowledge_proposals ADD COLUMN origin text NOT NULL DEFAULT 'original'
 CHECK(origin IN ('original','derived'));
CREATE TABLE core.knowledge_proposal_authorities (
 proposal_id bigint PRIMARY KEY REFERENCES core.knowledge_proposals(id),
 authorities jsonb NOT NULL CHECK(jsonb_typeof(authorities)='array' AND octet_length(authorities::text)<=1048576),
 revoked boolean NOT NULL DEFAULT false
);
