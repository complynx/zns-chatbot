-- Go runtime state is unreleased. This deliberately rejects a nonempty old
-- authority table; rebuild disposable development databases instead of guessing
-- a producing generation. Python originals do not populate this table.
ALTER TABLE core.conversation_read_authorities
 ADD COLUMN history_generation bigint NOT NULL CHECK(history_generation>=0);
