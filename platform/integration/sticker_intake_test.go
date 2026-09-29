package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/stickercache"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type assetDescriber struct {
	calls int
}

func (d *assetDescriber) Describe(_ context.Context, _ telegram.Message) (agent.AssetContext, error) {
	d.calls++
	return agent.AssetContext{
		Items: []agent.AssetObservation{{Kind: "sticker", Status: "described", Description: "A red circle."}},
	}, nil
}

func TestStickerIntakeAuthorizationContextAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	describer := &assetDescriber{}
	f.b.Stickers = describer
	update := message(100, 404, "")
	update.Message.Sticker = &telegram.Sticker{FileID: "file", UniqueID: "stable"}
	require.NoError(t, f.b.Handle(t.Context(), update))
	assert.Zero(t, describer.calls, "unknown Telegram identities must not reach artwork processing")
	update = message(101, 101, "")
	update.Message.Sticker = &telegram.Sticker{FileID: "file", UniqueID: "stable"}
	f.model.plan = agent.Plan{View: "workflow", Text: "This sticker shows a red circle."}
	handle(t, f.b, update)
	assert.Equal(t, 1, describer.calls)
	require.NotNil(t, f.model.input.Assets)
	assert.Equal(t, "A red circle.", f.model.input.Assets.Items[0].Description)
	assert.Empty(t, f.model.input.Text, "artwork must not be promoted to direct user instructions")
	handle(t, f.b, update)
	assert.Equal(t, 1, describer.calls, "durable plan replay must not bump or describe the same update again")
}

func TestStickerCacheBotRoleBoundary(t *testing.T) {
	t.Parallel()
	db := database(t)
	// Match the deployment's bot-owned schema in this fresh disposable database.
	_, err := db.Exec(t.Context(), "GRANT USAGE ON SCHEMA bot TO zns_bot")
	require.NoError(t, err)
	config := db.Config()
	config.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, roleErr := connection.Exec(ctx, "SET ROLE zns_bot")
		return roleErr
	}
	botDB, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(botDB.Close)
	cache, err := stickercache.New(botDB, stickercache.Options{})
	require.NoError(t, err)
	description, err := cache.Get(
		t.Context(),
		stickerKey("bot-role"),
		func(context.Context, stickercache.Key) (string, error) {
			return "A green square.", nil
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "A green square.", description)
	_, err = botDB.Exec(t.Context(), "UPDATE core.users SET name='forbidden' WHERE id='alice'")
	require.Error(t, err, "cache access must not grant access to authoritative domain tables")
}
