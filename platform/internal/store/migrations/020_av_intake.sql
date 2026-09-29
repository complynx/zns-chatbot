ALTER TABLE bot.media_intake ADD COLUMN av_kind text NOT NULL DEFAULT ''
  CHECK (av_kind IN ('','audio','voice','video','video_note'));
CREATE TABLE bot.av_results (
  intake_id text PRIMARY KEY REFERENCES bot.media_intake(id) ON DELETE CASCADE,
  status text NOT NULL, duration_num bigint NOT NULL DEFAULT 0,
  duration_den bigint NOT NULL DEFAULT 1 CHECK(duration_den>0),
  private_result jsonb,
  CHECK(private_result IS NULL OR octet_length(private_result::text)<=4194304)
);
CREATE TABLE bot.av_refinements (
  owner text NOT NULL, update_id bigint NOT NULL, rounds integer NOT NULL DEFAULT 0 CHECK(rounds BETWEEN 0 AND 2),
  PRIMARY KEY(owner,update_id)
);
