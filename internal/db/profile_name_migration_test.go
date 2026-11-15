package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProfileNameCleanupRemovesMarkupAndKeepsOtherText(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	provider := baselineProvider(t, s)
	_, err := provider.ApplyVersion(ctx, 81, false)
	require.NoError(t, err)
	type profile struct {
		full, first, last, source, latin string
		version                          int
		sync                             bool
	}
	for n, item := range []profile{
		{full: "@Anne_* D’Arcy"},
		{full: "@Анна Иванова*", first: "@Анна", last: "Иванова*", source: "user", latin: "Anna", sync: true},
		{full: "@ Иванова", first: "@", last: "Иванова", source: "user", latin: "At", sync: true},
		{full: "Anne-Marie D’Arcy", first: "Anne-Marie", last: "D’Arcy", source: "user", latin: "Anne", sync: true},
		{full: "@_*"},
	} {
		id := uuid.New()
		_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile)
 VALUES($1,1,$2,'{"first_name":"@observed"}')`, id, 800+n)
		require.NoError(t, err)
		_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_user_profiles
 (user_id,version,full_name,first_name,last_name,name_source,first_name_latin)
 VALUES($1,2,$2,$3,$4,$5,$6)`, id, item.full, item.first, item.last, item.source, item.latin)
		require.NoError(t, err)
		if item.sync {
			_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_profile_name_sync(user_id,desired_version,synced_version)
 VALUES($1,2,2)`, id)
			require.NoError(t, err)
		}
	}
	require.NoError(t, s.Migrate(ctx))
	rows, err := s.Pool().Query(ctx, `SELECT p.full_name,p.first_name,p.last_name,p.name_source,p.first_name_latin,
 p.version,n.user_id IS NOT NULL,u.profile->>'first_name'
 FROM shuttle_user_profiles p JOIN shuttle_users u ON u.id=p.user_id
 LEFT JOIN shuttle_profile_name_sync n ON n.user_id=p.user_id ORDER BY u.tg_user_id`)
	require.NoError(t, err)
	var got []profile
	for rows.Next() {
		var item profile
		var observed string
		require.NoError(t, rows.Scan(&item.full, &item.first, &item.last, &item.source, &item.latin,
			&item.version, &item.sync, &observed))
		require.Equal(t, "@observed", observed, "Telegram observations are not saved profile names")
		got = append(got, item)
	}
	require.NoError(t, rows.Err())
	require.Equal(t, []profile{
		{full: "Anne D’Arcy", version: 3},
		{full: "Анна Иванова", first: "Анна", last: "Иванова", source: "user", version: 3, sync: true},
		{full: "Иванова", version: 3},
		{full: "Anne-Marie D’Arcy", first: "Anne-Marie", last: "D’Arcy", source: "user", latin: "Anne",
			version: 2, sync: true},
		{version: 3},
	}, got)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_user_profiles SET full_name='@invalid' WHERE first_name=''`)
	require.ErrorContains(t, err, "shuttle_profile_name_no_markup")
	const versionsSQL = `SELECT string_agg(version::text,',' ORDER BY user_id) FROM shuttle_user_profiles`
	var before, after string
	require.NoError(t, s.Pool().QueryRow(ctx, versionsSQL).Scan(&before))
	_, err = provider.ApplyVersion(ctx, 81, false)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.Pool().QueryRow(ctx, versionsSQL).Scan(&after))
	require.Equal(t, before, after, "A repeated cleanup must not change clean names")
}
