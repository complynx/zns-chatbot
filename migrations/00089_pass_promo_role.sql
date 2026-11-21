-- +goose Up
-- NULL means this tier was available to both roles, including existing ticket choices.
ALTER TABLE shuttle_pass_promo_choices ADD COLUMN eligible_role text
    CHECK (eligible_role IN ('leader', 'follower'));
ALTER TABLE shuttle_pass_promo_choices ADD CHECK (eligible_role IS NULL OR awarded_role = eligible_role);

-- +goose Down
LOCK TABLE shuttle_pass_promo_choices IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT FROM shuttle_pass_promo_choices WHERE eligible_role IS NOT NULL) THEN
        RAISE EXCEPTION 'role-specific promo choices require explicit reconciliation before downgrade';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_pass_promo_choices DROP COLUMN eligible_role;
