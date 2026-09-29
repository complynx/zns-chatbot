ALTER TABLE core.users
 ADD COLUMN username text NOT NULL DEFAULT '' CHECK(char_length(username)<=64),
 ADD COLUMN first_name text NOT NULL DEFAULT '' CHECK(char_length(first_name)<=256),
 ADD COLUMN last_name text NOT NULL DEFAULT '' CHECK(char_length(last_name)<=256),
 ADD COLUMN print_name text NOT NULL DEFAULT '' CHECK(char_length(print_name)<=513),
 ADD COLUMN telegram_metadata_update bigint NOT NULL DEFAULT -1 CHECK(telegram_metadata_update>=-1);
