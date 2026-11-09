package db_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSymbolMigrationPreservesVersion74(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	task := accepted(t, s, 1)
	// Rewind the fixture's empty post-baseline additions as well as version 76.
	_, err := s.Pool().Exec(ctx, `DROP TABLE shuttle_symbols,shuttle_symbol_decay,
shuttle_reaction_cursors,shuttle_telegram_reactions,shuttle_user_bans,shuttle_telegram_availability;
DROP INDEX shuttle_tasks_symbols;
DROP FUNCTION shuttle_symbol_weight(double precision,double precision,double precision);
ALTER TABLE shuttle_users DROP COLUMN ban_public_reason;
DELETE FROM goose_db_version WHERE version_id>=76;
	INSERT INTO goose_db_version(version_id,is_applied) VALUES(74,true);`)
	require.NoError(t, err)
	_, err = s.Pool().
		Exec(ctx, `INSERT INTO shuttle_user_profiles(user_id,version,full_name,first_name,last_name,name_source,
 first_name_latin,last_name_latin) VALUES($1,1,'Анна Иванова','Анна','Иванова','user','Anna','Ivanova')`, task.UserID)
	require.NoError(t, err)
	var before, after string
	const query = `SELECT to_jsonb(p)::text FROM shuttle_user_profiles p`
	require.NoError(t, s.Pool().QueryRow(ctx, query).Scan(&before))
	var oidBefore, oidAfter uint32
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT 'shuttle_tasks'::regclass::oid").Scan(&oidBefore))
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.Pool().QueryRow(ctx, query).Scan(&after))
	require.Equal(t, before, after)
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT 'shuttle_tasks'::regclass::oid").Scan(&oidAfter))
	require.Equal(t, oidBefore, oidAfter)
	retained, err := s.GetTask(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, task, retained)
	var version int
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version").Scan(&version))
	require.Equal(t, 79, version)
}

func TestReactionMigrationPreservesVersion75CatalogAndTasks(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	task := accepted(t, s, 3)
	// The version-75 fixture predates both the baseline and the empty ban audit table.
	_, err := s.Pool().
		Exec(ctx, `DROP TABLE shuttle_reaction_cursors,shuttle_telegram_reactions,shuttle_user_bans,
shuttle_telegram_availability;
ALTER TABLE shuttle_users DROP COLUMN ban_public_reason;
DELETE FROM goose_db_version WHERE version_id>=76;
INSERT INTO goose_db_version(version_id,is_applied) VALUES(75,true);
INSERT INTO shuttle_symbols(bot_id,kind,asset_key,generation,reference,description,score,last_used_at)
VALUES(1,'sticker','retained','00000000-0000-0000-0000-000000000001',
'{"kind":"sticker","id":"retained","file_id":"source"}',
'{"description":"A happy cat","category":"emotion"}',7,now());`)
	require.NoError(t, err)
	var before, after string
	const query = "SELECT to_jsonb(s)::text FROM shuttle_symbols s"
	require.NoError(t, s.Pool().QueryRow(ctx, query).Scan(&before))
	var oidBefore, oidAfter uint32
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT 'shuttle_symbols'::regclass::oid").Scan(&oidBefore))
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.Pool().QueryRow(ctx, query).Scan(&after))
	require.Equal(t, before, after)
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT 'shuttle_symbols'::regclass::oid").Scan(&oidAfter))
	require.Equal(t, oidBefore, oidAfter)
	retained, err := s.GetTask(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, task, retained)
	var version int
	require.NoError(t, s.Pool().QueryRow(ctx, "SELECT max(version_id) FROM goose_db_version").Scan(&version))
	require.Equal(t, 79, version)
}
