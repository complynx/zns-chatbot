package db_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMessagePeopleScopeAndVisibility(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	owner, visible, hidden, revoked, foreign := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for n, id := range []uuid.UUID{owner, visible, hidden, revoked, foreign} {
		org := "org"
		if id == foreign {
			org = "other"
		}
		_, err := s.Pool().Exec(ctx, `INSERT INTO shuttle_users
 (id,bot_id,tg_user_id,zitadel_subject,zitadel_org,visible,revoked,profile)
 VALUES($1,123,$2,$3,$4,$5,$6,'{"first_name":"Telegram","last_name":"Name"}')`,
			id, int64(400+n), id.String(), org, id == visible || id == foreign, id == revoked)
		require.NoError(t, err)
	}
	_, err := s.Pool().
		Exec(ctx, `INSERT INTO shuttle_user_profiles(user_id,version,full_name) VALUES($1,1,'Profile Name')`, visible)
	require.NoError(t, err)
	scope := db.PaymentScope{
		BotID:        123,
		Organization: "org",
		UserID:       owner,
		Subject:      owner.String(),
	}
	ids := []uuid.UUID{owner, visible, hidden, revoked, foreign, visible}
	people, err := s.MessagePeople(ctx, scope, ids, []int64{401, 402, 404})
	require.NoError(t, err)
	require.Len(t, people, 4, "Exact basic identities include hidden and revoked targets")
	require.Equal(t, "Profile Name", people[visible.String()].FirstName)
	require.EqualValues(t, 400, people[owner.String()].ID)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_users SET profile=jsonb_build_object('first_name',$2::text) WHERE id=$1`,
		owner, strings.Repeat("Я", 300))
	require.NoError(t, err)
	people, err = s.MessagePeople(ctx, scope, []uuid.UUID{owner}, nil)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("Я", 256), people[owner.String()].FirstName)
	_, err = s.Pool().Exec(ctx, `WITH cleared AS (
 UPDATE shuttle_user_profiles SET full_name='' WHERE user_id=$1)
 UPDATE shuttle_users SET profile=jsonb_build_object('first_name',$2::text) WHERE id=$1`,
		visible, "Name "+hidden.String())
	require.NoError(t, err)
	people, err = s.MessagePeople(ctx, scope, []uuid.UUID{visible}, nil)
	require.NoError(t, err)
	require.Equal(t, "Telegram 401", people[visible.String()].FirstName,
		"An embedded account ID must not become another person's display identity")
	people, err = s.MessagePeople(ctx, scope, []uuid.UUID{uuid.New(), foreign}, []int64{999})
	require.NoError(t, err)
	require.Empty(t, people)
	scope.Subject = "wrong-subject"
	people, err = s.MessagePeople(ctx, scope, ids, nil)
	require.NoError(t, err)
	require.Empty(t, people)
}

func TestUsernameIndexPreservesPopulatedUpgradeAndRollback(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	ctx := t.Context()
	provider := baselineProvider(t, s)
	_, err := provider.DownTo(ctx, 80)
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx, `INSERT INTO shuttle_users
 (id,bot_id,tg_user_id,zitadel_org,zitadel_subject,profile) VALUES
 ($1,1,701,'org','one','{"username":"same_name"}'),
 ($2,1,702,'org','two','{"username":"SAME_NAME"}')`, uuid.New(), uuid.New())
	require.NoError(t, err)
	const dataSQL = `SELECT jsonb_agg(to_jsonb(u) ORDER BY id)::text FROM shuttle_users u`
	var before, after string
	require.NoError(t, s.Pool().QueryRow(ctx, dataSQL).Scan(&before))
	require.NoError(t, s.Migrate(ctx))
	require.NoError(t, s.Pool().QueryRow(ctx, dataSQL).Scan(&after))
	require.Equal(t, before, after, "The nonunique index must preserve duplicate usernames and all row data")
	var exists bool
	require.NoError(t, s.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_indexes
 WHERE schemaname=current_schema() AND indexname='shuttle_users_username_idx')`).Scan(&exists))
	require.True(t, exists)
	_, err = provider.DownTo(ctx, 80)
	require.NoError(t, err)
	require.NoError(t, s.Pool().QueryRow(ctx, dataSQL).Scan(&after))
	require.Equal(t, before, after)
	require.NoError(t, s.Migrate(ctx))
}
