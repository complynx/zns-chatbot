package db_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestPeopleNamesRestoreCopyWithEmptySearchPath(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	owner := uuid.New()
	_, err := s.Pool().Exec(ctx, `INSERT INTO shuttle_users(id,bot_id,tg_user_id,zitadel_subject,zitadel_org)
 VALUES($1,123,405,'person','org');`, owner)
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx,
		`INSERT INTO shuttle_user_profiles(user_id,version,full_name) VALUES($1,1,'Current Name')`, owner)
	require.NoError(t, err)
	var schema string
	require.NoError(t, s.Pool().QueryRow(ctx, `SELECT current_schema()`).Scan(&schema))
	tx, err := s.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SET LOCAL search_path=''`)
	require.NoError(t, err)
	// Logical restores qualify COPY's table but recompute generated columns with
	// an empty path. Helper functions must remain resolvable in that environment.
	copied, err := tx.CopyFrom(ctx, pgx.Identifier{schema, "shuttle_user_names"},
		[]string{"user_id", "name", "source"}, pgx.CopyFromRows([][]any{
			{owner, "Former Name", "profile"}, {owner, "old_username", "username"},
		}))
	require.NoError(t, err)
	require.EqualValues(t, 2, copied)
	require.NoError(t, tx.Commit(ctx))
	result, err := s.SearchPeople(ctx, db.PaymentScope{
		BotID: 123, Organization: "org", UserID: owner, Subject: "person", GlobalAdmin: true,
	}, db.PeopleQuery{Query: "Former Name"})
	require.NoError(t, err)
	require.Len(t, result.Matches, 1)
	require.True(t, result.Matches[0].Historical)
	require.Equal(t, owner, result.Matches[0].UserID)
}

func TestPeopleMigrationBackfillsNamesAndPreservesPrivacy(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	provider := baselineProvider(t, s)
	_, err := provider.ApplyVersion(t.Context(), 80, false)
	require.NoError(t, err)
	user := uuid.New()
	_, err = s.Pool().
		Exec(t.Context(), `INSERT INTO shuttle_users(id,bot_id,tg_user_id,zitadel_subject,zitadel_org,profile)
 VALUES($1,123,404,'person','org','{"first_name":"Telegram","last_name":"Name","username":"new_username"}');`, user)
	require.NoError(t, err)
	_, err = s.Pool().
		Exec(t.Context(), `INSERT INTO shuttle_user_profiles(user_id,version,full_name) VALUES($1,2,'Current Name')`, user)
	require.NoError(t, err)
	_, err = s.Pool().Exec(t.Context(), `INSERT INTO shuttle_profile_operations(user_id,operation_id,request_hash,result)
 VALUES($1,$2,decode(repeat('00',32),'hex'),'{"full_name":"Former Name","version":1}');`, user, uuid.New())
	require.NoError(t, err)
	var before string
	require.NoError(
		t,
		s.Pool().QueryRow(t.Context(), `SELECT to_jsonb(u)::text FROM shuttle_users u WHERE id=$1`, user).Scan(&before),
	)
	require.NoError(t, s.Migrate(t.Context()))
	var after string
	var visible bool
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), `SELECT (to_jsonb(u)-'visible')::text,visible FROM shuttle_users u WHERE id=$1`, user).
			Scan(&after, &visible),
	)
	require.Equal(t, before, after)
	require.True(t, visible)
	deployment := db.EventScope{BotID: 123, Organization: "org"}
	scope := db.PaymentScope{
		EventScope:  deployment,
		UserID:      user,
		Subject:     "person",
		GlobalAdmin: true,
	}
	for _, name := range []string{"Current Name", "Former Name", "Telegram Name", "new_username"} {
		result, searchErr := s.SearchPeople(t.Context(), scope, db.PeopleQuery{Query: name})
		require.NoError(t, searchErr)
		require.Len(t, result.Matches, 1)
	}
	_, err = s.Pool().
		Exec(t.Context(), `UPDATE shuttle_users SET visible=false,profile='{"first_name":"Renamed","username":"old_again"}'
 WHERE id=$1`, user)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(t.Context()))
	require.NoError(
		t,
		s.Pool().QueryRow(t.Context(), `SELECT visible FROM shuttle_users WHERE id=$1`, user).Scan(&visible),
	)
	require.False(t, visible)
	result, searchErr := s.SearchPeople(t.Context(), scope, db.PeopleQuery{Query: "Telegram Name"})
	require.NoError(t, searchErr)
	require.Len(t, result.Matches, 1)
	require.True(t, result.Matches[0].Historical)
	_, err = provider.ApplyVersion(t.Context(), 80, false)
	require.ErrorContains(t, err, "must be preserved")
}
