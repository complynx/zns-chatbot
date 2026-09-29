CREATE TABLE core.pass_profiles (
  owner text PRIMARY KEY REFERENCES core.users(id),
  version bigint NOT NULL DEFAULT 0 CHECK(version >= 0),
  role text NOT NULL DEFAULT '' CHECK(role IN ('','leader','follower')),
  legal_name text NOT NULL DEFAULT '' CHECK(char_length(legal_name) <= 300),
  passport text NOT NULL DEFAULT '' CHECK(char_length(passport) <= 300),
  frozen boolean NOT NULL DEFAULT false,
  pending text NOT NULL DEFAULT '' CHECK(pending IN ('','role','legal_name','passport')),
  expires_at timestamptz,
  passport_after boolean NOT NULL DEFAULT false,
  CHECK ((pending = '') = (expires_at IS NULL)),
  CHECK (NOT passport_after OR pending = 'legal_name')
);
CREATE TABLE core.pass_profile_operations (
  owner text NOT NULL REFERENCES core.users(id),
  key_hash text NOT NULL,
  request_hash text NOT NULL,
  PRIMARY KEY(owner,key_hash)
);
