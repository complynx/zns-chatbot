-- Application services own source-fenced Bot admissions and receipt projections.
DO $$
DECLARE runtime_role text;
BEGIN
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_api','zns_runtime') LOOP
  EXECUTE format('GRANT USAGE ON SCHEMA bot TO %I',runtime_role);
  EXECUTE format('GRANT SELECT,INSERT,UPDATE ON bot.delivery_intents,bot.interactions,bot.messages,bot.order_cards,bot.pass_views,bot.massage_views TO %I',runtime_role);
  EXECUTE format('GRANT SELECT,DELETE ON bot.pass_buttons,bot.massage_buttons TO %I',runtime_role);
  EXECUTE format('GRANT USAGE,SELECT ON SEQUENCE bot.delivery_effect_ids,bot.interactions_id_seq TO %I',runtime_role);
 END LOOP;
END $$;
