-- +goose Up
CREATE TABLE shuttle_pass_invites (
    code text PRIMARY KEY,
    event_id uuid NOT NULL REFERENCES shuttle_events(id),
    inviter_registration_id uuid NOT NULL,
    issue_operation text NOT NULL,
    claimant_user_id uuid REFERENCES shuttle_users(id),
    replace_pair_id uuid,
    pair_id uuid,
    revoked boolean NOT NULL DEFAULT false,
    FOREIGN KEY (inviter_registration_id,event_id) REFERENCES shuttle_registrations(id,event_id),
    FOREIGN KEY (replace_pair_id,event_id) REFERENCES shuttle_pairs(id,event_id),
    FOREIGN KEY (pair_id,event_id) REFERENCES shuttle_pairs(id,event_id),
    UNIQUE (inviter_registration_id, issue_operation),
    CHECK (pair_id IS NULL OR claimant_user_id IS NOT NULL)
);
CREATE UNIQUE INDEX shuttle_pass_invites_open ON shuttle_pass_invites(inviter_registration_id)
    WHERE pair_id IS NULL AND NOT revoked;
CREATE UNIQUE INDEX shuttle_pass_invites_claimed ON shuttle_pass_invites(event_id,claimant_user_id)
    WHERE pair_id IS NULL AND NOT revoked AND claimant_user_id IS NOT NULL;
CREATE INDEX shuttle_pass_invites_replacements ON shuttle_pass_invites(replace_pair_id)
    WHERE pair_id IS NULL AND NOT revoked;

-- +goose StatementBegin
CREATE FUNCTION shuttle_revoke_replacement_invites() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE shuttle_pass_invites SET revoked=true
    WHERE replace_pair_id=NEW.id AND pair_id IS NULL AND NOT revoked;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER shuttle_pass_invites_pair_end AFTER UPDATE OF state ON shuttle_pairs
FOR EACH ROW WHEN (OLD.state='waitlisted' AND NEW.state<>'waitlisted')
EXECUTE FUNCTION shuttle_revoke_replacement_invites();

UPDATE shuttle_pairs p SET queued_at=least(l.requested_at,f.requested_at)
FROM shuttle_registrations l, shuttle_registrations f
WHERE p.state='waitlisted' AND l.id=p.leader_registration_id AND f.id=p.follower_registration_id;

-- +goose Down
LOCK TABLE shuttle_pairs, shuttle_pass_invites IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT FROM shuttle_pass_invites) THEN
        RAISE EXCEPTION 'Pass invite history must be retained';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_pass_invites;
DROP TRIGGER shuttle_pass_invites_pair_end ON shuttle_pairs;
DROP FUNCTION shuttle_revoke_replacement_invites();
UPDATE shuttle_pairs p SET queued_at=greatest(l.requested_at,f.requested_at)
FROM shuttle_registrations l, shuttle_registrations f
WHERE p.state='waitlisted' AND l.id=p.leader_registration_id AND f.id=p.follower_registration_id;
