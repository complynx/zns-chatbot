-- +goose Up
-- A historical NULL means the original tier classification could not be verified.
ALTER TABLE shuttle_pass_tiers ADD COLUMN promo boolean;

UPDATE shuttle_pass_tiers t SET promo=true
FROM shuttle_passes p JOIN shuttle_registrations r ON r.id=p.registration_id
JOIN shuttle_pass_promo_choices c ON c.event_id=r.event_id AND c.user_id=r.user_id
WHERE p.id=t.pass_id AND c.awarded_role=r.role AND c.tier_id=t.tier_id;

-- A manual assignment's receipt identifies the issued pass. Its approved preparation
-- identifies the exact event revision, even when tiers have since been renamed or removed.
WITH verified AS (
    SELECT t.pass_id, COALESCE((tier.entry->>'promo')::boolean,false) AS promo
    FROM shuttle_registration_operations op
    JOIN shuttle_assignment_preparations prep USING(bot_id,organization_id,actor,operation_id)
    JOIN shuttle_event_operations revision ON revision.bot_id=prep.bot_id
        AND revision.organization_id=prep.organization_id
        AND revision.result->'id'=prep.document#>'{arguments,event_id}'
        AND revision.result->'version'=prep.document#>'{arguments,expected_event_version}'
    CROSS JOIN LATERAL jsonb_array_elements(revision.result#>'{document,registration,pricing,tiers}')
        WITH ORDINALITY AS tier(entry,number)
    JOIN shuttle_pass_tiers t ON t.pass_id::text=op.result#>>'{pass,id}'
    WHERE t.promo IS NULL AND op.result->'id'=prep.document#>'{arguments,registration_id}'
        AND op.result->'event_id'=prep.document#>'{arguments,event_id}'
        AND tier.entry->>'id'=t.tier_id AND tier.number=t.tier_number
        AND tier.entry->'price_minor'=op.result#>'{pass,price_minor}'
), classified AS (
    SELECT pass_id,bool_or(promo) AS promo FROM verified
    GROUP BY pass_id HAVING bool_and(promo)=bool_or(promo)
)
UPDATE shuttle_pass_tiers t SET promo=c.promo FROM classified c WHERE c.pass_id=t.pass_id;

-- +goose Down
LOCK TABLE shuttle_pass_tiers IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT FROM shuttle_pass_tiers WHERE promo IS TRUE) THEN
        RAISE EXCEPTION 'issued promo pass history requires reconciliation before downgrade';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_pass_tiers DROP COLUMN promo;
