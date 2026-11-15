package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProfileNameCleanupPreservesNamesAndOtherData(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	provider := baselineProvider(t, s)
	_, err := provider.ApplyVersion(ctx, 82, false)
	require.NoError(t, err)
	type fixture struct {
		full, first, last, source string
		latinFirst, latinLast     string
		changed                   bool
	}
	for n, item := range []fixture{
		{full: "@Anne_* D’Arcy", changed: true},
		{full: "@Анна Иванова*", first: "@Анна", last: "Иванова*", source: "user",
			latinFirst: "Anna", latinLast: "Ivanova", changed: true},
		{full: "@ Иванова", first: "@", last: "Иванова", source: "user",
			latinFirst: "At", latinLast: "Ivanova", changed: true},
		{full: "Anne-Marie D’Arcy A."},
		{full: "@_*", changed: true},
	} {
		id := uuid.New()
		_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile)
 VALUES($1,1,$2,'{"first_name":"@observed"}')`, id, 800+n)
		require.NoError(t, err)
		_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_user_profiles
 (user_id,version,full_name,first_name,last_name,name_source,bot_language,first_name_latin,last_name_latin)
 VALUES($1,2,$2,$3,$4,$5,'ru',$6,$7)`, id, item.full, item.first, item.last, item.source,
			item.latinFirst, item.latinLast)
		require.NoError(t, err)
		if item.first != "" {
			_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_profile_name_sync(user_id,desired_version,synced_version)
 VALUES($1,2,2)`, id)
			require.NoError(t, err)
		}
	}
	require.NoError(t, s.Migrate(ctx))
	rows, err := s.Pool().Query(ctx, `SELECT p.full_name,p.first_name,p.last_name,p.first_name_latin,p.last_name_latin,
 p.version,p.bot_language,
 u.profile->>'first_name',coalesce(n.desired_version,0)
 FROM shuttle_user_profiles p JOIN shuttle_users u ON u.id=p.user_id
 LEFT JOIN shuttle_profile_name_sync n ON n.user_id=p.user_id ORDER BY u.tg_user_id`)
	require.NoError(t, err)
	var got []fixture
	for rows.Next() {
		var item fixture
		var version, desired int
		var language, observed string
		require.NoError(t, rows.Scan(&item.full, &item.first, &item.last, &item.latinFirst, &item.latinLast,
			&version, &language, &observed, &desired))
		item.changed = version == 3
		require.Equal(t, "ru", language)
		require.Equal(t, "@observed", observed, "Raw Telegram observations are not profile preferences")
		if item.first != "" {
			require.Equal(t, version, desired)
		} else {
			require.Zero(t, desired)
		}
		got = append(got, item)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	require.Equal(t, []fixture{
		{full: "Anne D’Arcy", changed: true},
		{full: "Анна Иванова", first: "Анна", last: "Иванова",
			latinFirst: "Anna", latinLast: "Ivanova", changed: true},
		{full: "Иванова", changed: true},
		{full: "Anne-Marie D’Arcy A."},
		{changed: true},
	}, got)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_user_profiles SET full_name='@invalid' WHERE first_name=''`)
	require.ErrorContains(t, err, "shuttle_profile_name_no_markup")
	const snapshotSQL = `SELECT jsonb_agg(to_jsonb(p) ORDER BY user_id)::text FROM shuttle_user_profiles p`
	var before, after string
	require.NoError(t, s.Pool().QueryRow(ctx, snapshotSQL).Scan(&before))
	_, err = provider.ApplyVersion(ctx, 82, false)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.Pool().QueryRow(ctx, snapshotSQL).Scan(&after))
	require.Equal(t, before, after, "Rollback/reapply retains the approved cleanup and current revisions")
}
