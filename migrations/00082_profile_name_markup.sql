-- +goose Up
-- The approved cleanup removes only markup delimiters and @ from saved profile
-- names. Telegram observations and immutable operation receipts stay unchanged.
CREATE TEMP TABLE shuttle_profile_name_cleanup ON COMMIT DROP AS
SELECT user_id, first_name_latin, last_name_latin,
       btrim(translate(full_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')) AS full_name,
       btrim(translate(first_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')) AS first_name,
       btrim(translate(last_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')) AS last_name
FROM shuttle_user_profiles
WHERE translate(full_name || first_name || last_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')
      <> full_name || first_name || last_name;

UPDATE shuttle_user_profiles p
SET full_name = CASE WHEN c.first_name <> '' AND c.last_name <> ''
                    THEN c.first_name || ' ' || c.last_name ELSE c.full_name END,
    first_name = CASE WHEN c.first_name <> '' AND c.last_name <> '' THEN c.first_name ELSE '' END,
    last_name = CASE WHEN c.first_name <> '' AND c.last_name <> '' THEN c.last_name ELSE '' END,
    name_source = CASE WHEN c.first_name <> '' AND c.last_name <> '' THEN p.name_source ELSE '' END,
    version = p.version + 1
FROM shuttle_profile_name_cleanup c WHERE c.user_id = p.user_id;

-- Ordinary name edits invalidate Latin spellings through the existing trigger.
-- Mechanical delimiter removal preserves them when both name components survive.
UPDATE shuttle_user_profiles p
SET first_name_latin = c.first_name_latin, last_name_latin = c.last_name_latin
FROM shuttle_profile_name_cleanup c
WHERE c.user_id = p.user_id AND p.first_name <> '' AND p.last_name <> '';

-- Existing sync jobs must refer to the corrected revision. If cleanup removes a
-- whole component, keep a display-only name and stop sending an invalid IAM name.
UPDATE shuttle_profile_name_sync n
SET desired_version = p.version, next_attempt_at = clock_timestamp(), updated_at = clock_timestamp()
FROM shuttle_user_profiles p JOIN shuttle_profile_name_cleanup c ON c.user_id = p.user_id
WHERE n.user_id = p.user_id AND p.first_name <> '' AND p.last_name <> '';

DELETE FROM shuttle_profile_name_sync n
USING shuttle_user_profiles p, shuttle_profile_name_cleanup c
WHERE n.user_id = p.user_id AND c.user_id = p.user_id AND p.first_name = '';

ALTER TABLE shuttle_user_profiles ADD CONSTRAINT shuttle_profile_name_no_markup CHECK (
    translate(full_name || first_name || last_name, $blocked$\`*_[](){}<>#|~@!$blocked$, '')
    = full_name || first_name || last_name
);

-- +goose Down
-- Rollback permits the old input format; the explicitly approved removal of
-- characters is retained, just like a subsequent user edit to a name.
ALTER TABLE shuttle_user_profiles DROP CONSTRAINT shuttle_profile_name_no_markup;
