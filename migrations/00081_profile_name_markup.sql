-- +goose Up
-- Remove Markdown delimiters and @ from saved profile names, then reject them.
-- The cleanup acts like a name edit: the revision increases and the existing
-- trigger clears the Latin spelling. For a profile with a name sync row, the
-- periodic sync sends the cleaned name to IAM. A name imported from IAM has no
-- such row and stays unchanged in IAM. Telegram observations and operation
-- receipts stay unchanged.
UPDATE shuttle_user_profiles p
SET full_name = CASE WHEN c.first_name <> '' AND c.last_name <> ''
                    THEN c.first_name || ' ' || c.last_name ELSE c.full_name END,
    first_name = CASE WHEN c.first_name <> '' AND c.last_name <> '' THEN c.first_name ELSE '' END,
    last_name = CASE WHEN c.first_name <> '' AND c.last_name <> '' THEN c.last_name ELSE '' END,
    name_source = CASE WHEN c.first_name <> '' AND c.last_name <> '' THEN p.name_source ELSE '' END,
    version = p.version + 1
FROM (
    SELECT user_id,
           btrim(translate(full_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')) AS full_name,
           btrim(translate(first_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')) AS first_name,
           btrim(translate(last_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')) AS last_name
    FROM shuttle_user_profiles
    WHERE translate(full_name || first_name || last_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')
          <> full_name || first_name || last_name
) c
WHERE c.user_id = p.user_id;

-- A name that lost a whole component is display-only. It has no IAM name to send.
DELETE FROM shuttle_profile_name_sync n
USING shuttle_user_profiles p
WHERE p.user_id = n.user_id AND p.first_name = '';

ALTER TABLE shuttle_user_profiles ADD CONSTRAINT shuttle_profile_name_no_markup CHECK (
    translate(full_name || first_name || last_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')
    = full_name || first_name || last_name
);

-- +goose Down
-- Rollback permits the old input format again. The cleaned names stay.
ALTER TABLE shuttle_user_profiles DROP CONSTRAINT shuttle_profile_name_no_markup;
