-- Broadcast publication keeps its immutable source on the draft/input, while
-- recipient and delivery rows inherit it through their existing message FK.
ALTER TABLE core.admin_messages
 ADD COLUMN source_origin text NOT NULL DEFAULT 'original',
 ADD COLUMN source_authorities jsonb NOT NULL DEFAULT '[]',
 ADD COLUMN source_revoked boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT admin_message_source_shape CHECK (
  jsonb_typeof(source_authorities)='array' AND
  ((source_origin='original' AND source_authorities='[]'::jsonb AND NOT source_revoked) OR
   (source_origin='derived' AND jsonb_array_length(source_authorities) BETWEEN 1 AND 256))
 );
ALTER TABLE core.admin_message_inputs
 ADD COLUMN source_origin text NOT NULL DEFAULT 'original',
 ADD COLUMN source_authorities jsonb NOT NULL DEFAULT '[]',
 ADD COLUMN source_revoked boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT admin_message_input_source_shape CHECK (
  jsonb_typeof(source_authorities)='array' AND
  ((source_origin='original' AND source_authorities='[]'::jsonb AND NOT source_revoked) OR
   (source_origin='derived' AND jsonb_array_length(source_authorities) BETWEEN 1 AND 256))
 );

