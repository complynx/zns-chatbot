-- +goose Up
-- These keys let membership constraints enforce a common event and dance role.
ALTER TABLE shuttle_registrations ADD CONSTRAINT shuttle_registrations_id_event UNIQUE(id,event_id);
ALTER TABLE shuttle_registrations ADD CONSTRAINT shuttle_registrations_id_event_role UNIQUE(id,event_id,role);
CREATE TABLE shuttle_pairs (
 id uuid PRIMARY KEY,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 leader_registration_id uuid NOT NULL,
 follower_registration_id uuid NOT NULL,
 state text NOT NULL DEFAULT 'waitlisted' CHECK(state IN ('waitlisted','assigned','dissolved')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 queued_at timestamptz NOT NULL, created_at timestamptz NOT NULL,
 assigned_at timestamptz, dissolved_at timestamptz,
 UNIQUE(id,event_id),
 CHECK(leader_registration_id<>follower_registration_id),
 CHECK((state='dissolved')=(dissolved_at IS NOT NULL)),
 CHECK(state<>'assigned' OR assigned_at IS NOT NULL),
 FOREIGN KEY(leader_registration_id,event_id) REFERENCES shuttle_registrations(id,event_id),
 FOREIGN KEY(follower_registration_id,event_id) REFERENCES shuttle_registrations(id,event_id)
);
CREATE TABLE shuttle_active_pair_members (
 registration_id uuid PRIMARY KEY,
 pair_id uuid NOT NULL,
 event_id uuid NOT NULL,
 role text NOT NULL CHECK(role IN ('leader','follower')),
 UNIQUE(pair_id,role),
 FOREIGN KEY(pair_id,event_id) REFERENCES shuttle_pairs(id,event_id),
 FOREIGN KEY(registration_id,event_id,role) REFERENCES shuttle_registrations(id,event_id,role)
);
CREATE TABLE shuttle_pair_invitations (
 id uuid PRIMARY KEY,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 inviter_registration_id uuid NOT NULL,
 target_user_id uuid NOT NULL REFERENCES shuttle_users(id),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','accepted','declined','revoked','expired')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 decided_at timestamptz, pair_id uuid,
 CHECK(expires_at>created_at),
 CHECK((state='pending')=(decided_at IS NULL)),
 CHECK((state='accepted')=(pair_id IS NOT NULL)),
 FOREIGN KEY(pair_id,event_id) REFERENCES shuttle_pairs(id,event_id),
 FOREIGN KEY(inviter_registration_id,event_id) REFERENCES shuttle_registrations(id,event_id)
);
CREATE UNIQUE INDEX shuttle_pair_invitations_outgoing ON shuttle_pair_invitations(inviter_registration_id)
WHERE state='pending';
CREATE INDEX shuttle_pair_invitations_incoming ON shuttle_pair_invitations(event_id,target_user_id,id);
CREATE TABLE shuttle_pair_operations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, kind text NOT NULL,
 request_hash bytea NOT NULL, result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
CREATE TABLE shuttle_pair_preparations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, proposal_id uuid NOT NULL UNIQUE,
 request_hash bytea NOT NULL, expires_at timestamptz NOT NULL, document jsonb NOT NULL,
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
-- +goose Down
-- Binary rollback retains pair history/receipts. Export before intentional schema rollback.
DROP TABLE shuttle_pair_preparations;
DROP TABLE shuttle_pair_operations;
DROP TABLE shuttle_pair_invitations;
DROP TABLE shuttle_active_pair_members;
DROP TABLE shuttle_pairs;
ALTER TABLE shuttle_registrations DROP CONSTRAINT shuttle_registrations_id_event_role;
ALTER TABLE shuttle_registrations DROP CONSTRAINT shuttle_registrations_id_event;
