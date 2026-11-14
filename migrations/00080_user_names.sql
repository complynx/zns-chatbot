-- +goose Up
ALTER TABLE shuttle_users ADD COLUMN visible boolean NOT NULL DEFAULT true;
ALTER TABLE shuttle_user_profiles
    ADD COLUMN gender text NOT NULL DEFAULT '' CHECK (char_length(gender)<=128),
    ADD COLUMN pronouns text NOT NULL DEFAULT '' CHECK (char_length(pronouns)<=128),
    ADD COLUMN gender_source text NOT NULL DEFAULT '' CHECK (gender_source IN ('','explicit','inferred')),
    ADD COLUMN pronouns_source text NOT NULL DEFAULT '' CHECK (pronouns_source IN ('','explicit','inferred')),
    ADD CONSTRAINT shuttle_profile_gender_source CHECK (
        (gender='')=(gender_source='') AND (pronouns='')=(pronouns_source=''));
-- No extensions are required. Keep one normalizer for candidate keys and ranking.
-- +goose StatementBegin
CREATE FUNCTION shuttle_name_tokens(value text) RETURNS text[]
LANGUAGE plpgsql IMMUTABLE STRICT PARALLEL SAFE AS $$
DECLARE
    alphabet text := 'абвгдеёжзийклмнопрстуфхцчшщъыьэюяіїєґ';
    latin text[] := ARRAY['a','b','v','g','d','e','e','zh','z','i','y','k','l','m','n','o','p','r','s',
        't','u','f','kh','ts','ch','sh','shch','','y','','e','yu','ya','i','i','ye','g'];
    folded text := '';
    letter text;
    position integer;
BEGIN
    FOREACH letter IN ARRAY regexp_split_to_array(normalize(lower(value COLLATE pg_catalog.pg_c_utf8), NFC), '') LOOP
        position := strpos(alphabet, letter);
        folded := folded || CASE WHEN position>0 THEN latin[position] ELSE letter END;
    END LOOP;
    -- Strip decomposed accent marks, then separate punctuation and canonicalize
    -- common Latin spellings (Maxim/Maksim, Dmitry/Dmitriy/Dmitrii).
    folded := regexp_replace(normalize(folded, NFD), U&'[\0300-\036f\1ab0-\1aff\1dc0-\1dff\20d0-\20ff\fe20-\fe2f]', '', 'g');
    RETURN ARRAY(SELECT regexp_replace(replace(token,'x','ks'), '(i*y|i{2,})$', 'i')
        FROM regexp_split_to_table(folded COLLATE pg_catalog.pg_c_utf8, '[^[:alnum:]]+') token WHERE token<>'' ORDER BY 1);
END $$;
-- +goose StatementEnd

-- Bigrams admit prefixes/substrings. Shared deletion keys admit up to two typos
-- or adjacent swaps. Initial keys admit one-letter query components. Long tokens use bigrams only; fuzzy ranking has the same bound.
-- +goose StatementBegin
CREATE FUNCTION shuttle_name_keys(value text, key_prefix text DEFAULT '') RETURNS text[]
LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
BEGIN ATOMIC
    WITH tokens AS (SELECT unnest(shuttle_name_tokens(value)) AS token),
    first_deletion AS (
        SELECT DISTINCT left(token,n-1)||substr(token,n+1) AS token FROM tokens,
        LATERAL generate_series(1,length(token)) n WHERE length(token) BETWEEN 4 AND 32
    ), second_deletion AS (
        SELECT DISTINCT left(token,n-1)||substr(token,n+1) AS token FROM first_deletion,
        LATERAL generate_series(1,length(token)) n WHERE length(token)>=6
    ), keys AS (
        SELECT 'f:'||token AS key FROM tokens
        UNION SELECT 'i:'||left(token,1) FROM tokens
        UNION SELECT 'f:'||token FROM first_deletion
        UNION SELECT 'f:'||token FROM second_deletion
        UNION SELECT 'g:'||substr(token,n,2) FROM tokens,
            LATERAL generate_series(1,length(token)-1) n
    ) SELECT coalesce(array_agg(key ORDER BY key), ARRAY[]::text[]) FROM keys WHERE starts_with(key,key_prefix);
END;
-- +goose StatementEnd

CREATE INDEX shuttle_tasks_user_activity_idx ON shuttle_tasks(user_id,created_at DESC);

CREATE TABLE shuttle_user_names (
    user_id uuid NOT NULL REFERENCES shuttle_users(id),
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 512),
    search_keys text[] GENERATED ALWAYS AS (shuttle_name_keys(name)) STORED,
    source text NOT NULL CHECK (source IN ('profile','latin','telegram','username')),
    PRIMARY KEY (user_id, source, name)
);

CREATE INDEX shuttle_user_names_search_idx ON shuttle_user_names USING gin(search_keys);

-- Retain observed names across all profile writers, including intake and imports.
-- +goose StatementBegin
CREATE FUNCTION shuttle_remember_user_names() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_TABLE_NAME = 'shuttle_users' THEN
        INSERT INTO shuttle_user_names(user_id,name,source)
        SELECT NEW.id,left(btrim(v.name),512),v.source
        FROM (VALUES (concat_ws(' ',NEW.profile->>'first_name',NEW.profile->>'last_name'),'telegram'),
                     (NEW.profile->>'username','username')) v(name,source)
        WHERE btrim(coalesce(v.name,''))<>'' AND NOT EXISTS (
            SELECT 1 FROM shuttle_user_names n WHERE n.user_id=NEW.id
                AND n.source=v.source AND n.name=left(btrim(v.name),512))
        ON CONFLICT DO NOTHING;
    ELSE
        INSERT INTO shuttle_user_names(user_id,name,source)
        SELECT NEW.user_id,left(btrim(v.name),512),v.source
        FROM (VALUES (NEW.full_name,'profile'),
                     (concat_ws(' ',NEW.first_name_latin,NEW.last_name_latin),'latin')) v(name,source)
        WHERE btrim(coalesce(v.name,''))<>'' AND NOT EXISTS (
            SELECT 1 FROM shuttle_user_names n WHERE n.user_id=NEW.user_id
                AND n.source=v.source AND n.name=left(btrim(v.name),512))
        ON CONFLICT DO NOTHING;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER shuttle_users_remember_names AFTER INSERT OR UPDATE OF profile ON shuttle_users
FOR EACH ROW EXECUTE FUNCTION shuttle_remember_user_names();
CREATE TRIGGER shuttle_profiles_remember_names AFTER INSERT OR UPDATE OF full_name,first_name_latin,last_name_latin
ON shuttle_user_profiles FOR EACH ROW EXECUTE FUNCTION shuttle_remember_user_names();

INSERT INTO shuttle_user_names(user_id,name,source)
SELECT DISTINCT user_id,left(btrim(name),512),source FROM (
    SELECT id AS user_id,concat_ws(' ',profile->>'first_name',profile->>'last_name') AS name,'telegram' AS source
    FROM shuttle_users
    UNION ALL SELECT id,profile->>'username','username' FROM shuttle_users
    UNION ALL SELECT user_id,full_name,'profile' FROM shuttle_user_profiles
    UNION ALL SELECT user_id,concat_ws(' ',first_name_latin,last_name_latin),'latin' FROM shuttle_user_profiles
    UNION ALL SELECT user_id,result->>'full_name','profile' FROM shuttle_profile_operations
    UNION ALL SELECT user_id,concat_ws(' ',result->>'first_name_latin',result->>'last_name_latin'),'latin'
    FROM shuttle_profile_operations
    UNION ALL SELECT user_id,concat_ws(' ',request#>>'{user,first_name}',request#>>'{user,last_name}'),'telegram'
    FROM shuttle_tasks
    UNION ALL SELECT user_id,request#>>'{user,username}','username' FROM shuttle_tasks
) names WHERE btrim(coalesce(name,''))<>'' ON CONFLICT DO NOTHING;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shuttle_user_names) OR EXISTS (SELECT 1 FROM shuttle_users WHERE NOT visible)
       OR EXISTS (SELECT 1 FROM shuttle_user_profiles WHERE gender<>'' OR pronouns<>'') THEN
        RAISE EXCEPTION 'Observed names, visibility and gender preferences must be preserved; use a reviewed forward migration';
    END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER shuttle_users_remember_names ON shuttle_users;
DROP TRIGGER shuttle_profiles_remember_names ON shuttle_user_profiles;
DROP FUNCTION shuttle_remember_user_names();
DROP TABLE shuttle_user_names;
DROP INDEX shuttle_tasks_user_activity_idx;
DROP FUNCTION shuttle_name_keys(text,text);
DROP FUNCTION shuttle_name_tokens(text);
ALTER TABLE shuttle_users DROP COLUMN visible;
ALTER TABLE shuttle_user_profiles DROP CONSTRAINT shuttle_profile_gender_source,
    DROP COLUMN gender, DROP COLUMN pronouns, DROP COLUMN gender_source, DROP COLUMN pronouns_source;
