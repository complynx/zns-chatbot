CREATE TABLE core.pass_profile_history (
  owner text NOT NULL REFERENCES core.users(id),
  version bigint NOT NULL CHECK(version > 0),
  action text NOT NULL CHECK(action IN ('begin','submit','set','cancel')),
  field text NOT NULL CHECK(field IN ('','role','legal_name','passport')),
  origin text NOT NULL CHECK(origin IN ('manual','agent')),
  at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY(owner,version)
);
