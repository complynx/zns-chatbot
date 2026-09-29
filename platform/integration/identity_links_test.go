package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestIdentityLinksNeverMergeUsers(t *testing.T) {
	t.Parallel()
	db := database(t)
	links := identity.Links{DB: db, Issuer: "https://id.example.invalid", BotID: 123}
	ctx := t.Context()
	require.NoError(t, links.Bind(ctx, "alice", 101, "z-alice"))
	require.NoError(t, links.Bind(ctx, "alice", 101, "z-alice"))
	user, err := links.Telegram(ctx, 101)
	require.NoError(t, err)
	assert.Equal(t, identity.User{Owner: "alice", Subject: "z-alice"}, user)
	require.ErrorIs(t, links.Bind(ctx, "bob", 202, "z-alice"), identity.ErrZitadelIdentity)
	require.ErrorIs(t, links.Bind(ctx, "alice", 101, "replacement"), identity.ErrZitadelIdentity)
	require.ErrorIs(t, links.Bind(ctx, "bob", 101, "z-bob"), identity.ErrZitadelIdentity)
	_, err = links.Telegram(ctx, 202)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	other := links
	other.BotID = 124
	_, err = other.Telegram(ctx, 101)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	other = links
	other.Issuer = "https://different.invalid"
	_, err = other.Subject(ctx, "z-alice")
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	_, err = db.Exec(ctx, `UPDATE core.zitadel_identities SET active=false WHERE owner='alice'`)
	require.NoError(t, err)
	_, err = links.Telegram(ctx, 101)
	require.ErrorIs(t, err, identity.ErrZitadelIdentity)
	require.ErrorIs(t, links.Bind(ctx, "alice", 101, "z-alice"), identity.ErrZitadelIdentity)
}
