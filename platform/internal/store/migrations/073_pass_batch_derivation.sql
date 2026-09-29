ALTER TABLE core.pass_admin_batches
    ADD COLUMN source_derivation jsonb
    CHECK (source_derivation IS NULL OR jsonb_typeof(source_derivation) = 'object');
