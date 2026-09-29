-- Transport ordering is shared; payload, eligibility and attempt fencing stay
-- with the referenced owner. No intermediate Go outbox state is adopted here.
CREATE TABLE core.delivery_lanes (
 bot_id bigint NOT NULL CHECK(bot_id>0),
 chat text NOT NULL CHECK(chat ~ '^-?[1-9][0-9]*$'),
 next_sequence bigint NOT NULL DEFAULT 0 CHECK(next_sequence>=0),
 last_served bigint NOT NULL DEFAULT 0 CHECK(last_served>=0),
 PRIMARY KEY(bot_id,chat)
);
CREATE TABLE core.delivery_queue (
 bot_id bigint NOT NULL,
 owner_kind text NOT NULL CHECK(owner_kind IN ('orders','passes','food','massage','admin','announcement','bot')),
 owner_key text NOT NULL CHECK(length(owner_key) BETWEEN 1 AND 200),
 effect_key text NOT NULL CHECK(length(effect_key) BETWEEN 1 AND 100),
 chat text NOT NULL,
 thread_id bigint NOT NULL DEFAULT 0 CHECK(thread_id>=0),
 lane_sequence bigint NOT NULL CHECK(lane_sequence>0),
 traffic_class text NOT NULL CHECK(traffic_class IN ('interactive','background')),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sending','sent','failed','cancelled','unknown','parked','paused')),
 not_before timestamptz NOT NULL DEFAULT '-infinity',
 PRIMARY KEY(bot_id,owner_kind,owner_key,effect_key),
 UNIQUE(bot_id,chat,lane_sequence),
 FOREIGN KEY(bot_id,chat) REFERENCES core.delivery_lanes(bot_id,chat)
);
CREATE INDEX delivery_queue_active_head ON core.delivery_queue(bot_id,chat,lane_sequence)
 WHERE state IN ('pending','sending','unknown','parked','paused');
CREATE TABLE core.delivery_fairness (
 bot_id bigint PRIMARY KEY CHECK(bot_id>0),
 grants bigint NOT NULL DEFAULT 0 CHECK(grants>=0)
);
DO $$
DECLARE runtime_role text;
BEGIN
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_api','zns_runtime') LOOP
  EXECUTE format('GRANT SELECT,INSERT,UPDATE ON core.delivery_lanes,core.delivery_queue,core.delivery_fairness TO %I',runtime_role);
 END LOOP;
END $$;
