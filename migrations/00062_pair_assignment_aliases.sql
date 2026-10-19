-- +goose Up
CREATE TABLE shuttle_pair_assignment_aliases (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL CHECK(length(operation_id) BETWEEN 1 AND 128),
 canonical_operation_id text NOT NULL,
 PRIMARY KEY(bot_id,organization_id,actor,operation_id),
 FOREIGN KEY(bot_id,organization_id,actor,canonical_operation_id)
 REFERENCES shuttle_pair_preparations(bot_id,organization_id,actor,operation_id),
 CHECK(operation_id<>canonical_operation_id)
);

-- Guard the shared transport namespace even when the previous binary writes a
-- canonical preparation without knowing about aliases. Both writers take the
-- same transaction advisory lock as the application before testing the other table.
-- +goose StatementBegin
CREATE FUNCTION shuttle_guard_pair_assignment_namespace() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(format('pair:%s:%s:%s:%s',
  NEW.bot_id,NEW.organization_id,NEW.actor,NEW.operation_id),0));
 IF TG_TABLE_NAME='shuttle_pair_assignment_aliases' THEN
  IF EXISTS(SELECT 1 FROM shuttle_pair_preparations p WHERE p.bot_id=NEW.bot_id
   AND p.organization_id=NEW.organization_id AND p.actor=NEW.actor AND p.operation_id=NEW.operation_id)
   OR EXISTS(SELECT 1 FROM shuttle_pair_operations r WHERE r.bot_id=NEW.bot_id
   AND r.organization_id=NEW.organization_id AND r.actor=NEW.actor AND r.operation_id=NEW.operation_id) THEN
   RAISE EXCEPTION 'assignment alias key is already occupied' USING ERRCODE='23505';
  END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM shuttle_pair_assignment_aliases a WHERE a.bot_id=NEW.bot_id
   AND a.organization_id=NEW.organization_id AND a.actor=NEW.actor AND a.operation_id=NEW.operation_id) THEN
   RAISE EXCEPTION 'assignment operation is already an alias' USING ERRCODE='23505';
  END IF;
  IF TG_TABLE_NAME='shuttle_pair_preparations' THEN
   IF EXISTS(SELECT 1 FROM shuttle_pair_operations r WHERE r.bot_id=NEW.bot_id
    AND r.organization_id=NEW.organization_id AND r.actor=NEW.actor AND r.operation_id=NEW.operation_id
    AND (r.kind<>'assign' OR r.request_hash<>NEW.request_hash)) THEN
    RAISE EXCEPTION 'assignment key has a different pair receipt' USING ERRCODE='23505';
   END IF;
  ELSE
   IF EXISTS(SELECT 1 FROM shuttle_pair_preparations p WHERE p.bot_id=NEW.bot_id
    AND p.organization_id=NEW.organization_id AND p.actor=NEW.actor AND p.operation_id=NEW.operation_id
    AND (NEW.kind<>'assign' OR p.request_hash<>NEW.request_hash)) THEN
    RAISE EXCEPTION 'pair receipt key has a different assignment preparation' USING ERRCODE='23505';
   END IF;
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER shuttle_pair_assignment_preparation_namespace BEFORE INSERT ON shuttle_pair_preparations
 FOR EACH ROW EXECUTE FUNCTION shuttle_guard_pair_assignment_namespace();
CREATE TRIGGER shuttle_pair_assignment_alias_namespace BEFORE INSERT ON shuttle_pair_assignment_aliases
 FOR EACH ROW EXECUTE FUNCTION shuttle_guard_pair_assignment_namespace();
CREATE TRIGGER shuttle_pair_receipt_namespace BEFORE INSERT ON shuttle_pair_operations
 FOR EACH ROW EXECUTE FUNCTION shuttle_guard_pair_assignment_namespace();

-- +goose Down
LOCK TABLE shuttle_pair_assignment_aliases IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_pair_assignment_aliases) THEN
  RAISE EXCEPTION 'export and deliberately remove pair assignment aliases before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TRIGGER shuttle_pair_assignment_preparation_namespace ON shuttle_pair_preparations;
DROP TRIGGER shuttle_pair_receipt_namespace ON shuttle_pair_operations;
DROP TABLE shuttle_pair_assignment_aliases;
DROP FUNCTION shuttle_guard_pair_assignment_namespace();
