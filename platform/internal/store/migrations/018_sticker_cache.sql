CREATE TABLE core.sticker_descriptions (
  kind text NOT NULL CHECK (kind IN ('sticker', 'custom_emoji')),
  asset_id text NOT NULL CHECK (octet_length(asset_id) BETWEEN 1 AND 512),
  descriptor_version text NOT NULL CHECK (octet_length(descriptor_version) BETWEEN 1 AND 128),
  score double precision NOT NULL DEFAULT 0 CHECK (score >= 0),
  scored_at timestamptz NOT NULL,
  description text CHECK (octet_length(description) BETWEEN 1 AND 8192),
  PRIMARY KEY (kind, asset_id, descriptor_version)
);
CREATE INDEX sticker_descriptions_resident ON core.sticker_descriptions(scored_at)
  WHERE description IS NOT NULL;
