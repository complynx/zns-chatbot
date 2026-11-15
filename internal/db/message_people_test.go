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
	people, needsAdmin, err := s.MessagePeople(ctx, scope, ids, []int64{401, 402, 404})
	require.NoError(t, err)
	require.Len(t, people, 2)
	require.True(t, needsAdmin, "Only the hidden account needs privileged resolution")
	require.Equal(t, "Profile Name", people[visible.String()].FirstName)
	require.EqualValues(t, 400, people[owner.String()].ID)
	_, err = s.Pool().Exec(ctx, `UPDATE shuttle_users SET profile=jsonb_build_object('first_name',$2::text) WHERE id=$1`,
		owner, strings.Repeat("Я", 300))
	require.NoError(t, err)
	people, _, err = s.MessagePeople(ctx, scope, []uuid.UUID{owner}, nil)
	require.NoError(t, err)
	require.Equal(t, strings.Repeat("Я", 256), people[owner.String()].FirstName)
	_, needsAdmin, err = s.MessagePeople(ctx, scope, []uuid.UUID{uuid.New(), revoked, foreign}, []int64{999})
	require.NoError(t, err)
	require.False(t, needsAdmin, "Non-person, revoked and foreign IDs must not trigger IAM")
	scope.GlobalAdmin = true
	people, needsAdmin, err = s.MessagePeople(ctx, scope, ids, nil)
	require.NoError(t, err)
	require.Len(t, people, 3)
	require.False(t, needsAdmin)
	require.EqualValues(t, 402, people[hidden.String()].ID)
	require.NotContains(t, people, foreign.String())
	require.NotContains(t, people, revoked.String())
	scope.Subject = "wrong-subject"
	people, needsAdmin, err = s.MessagePeople(ctx, scope, ids, nil)
	require.NoError(t, err)
	require.Empty(t, people)
	require.False(t, needsAdmin)
}
