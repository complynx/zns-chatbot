package integration_test

import (
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

func TestModelSettingsPermissionsAndInheritance(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := modelsettings.Service{DB: db}
	ctx := t.Context()
	change := func(model, key string, version int64) modelsettings.Change {
		return modelsettings.Change{
			Model: model, Effort: "high",
			Version:      version,
			OperationKey: key,
		}
	}
	_, err := s.Read(ctx, "alice", "alice")
	requireCode(t, err, "forbidden")
	requireCode(
		t,
		s.Grant(ctx, "alice", modelsettings.Grant{Owner: "alice", Capability: "own", Enabled: true}),
		"forbidden",
	)
	require.NoError(t, s.Grant(ctx, "bob", modelsettings.Grant{Owner: "alice", Capability: "own", Enabled: true}))
	own := change("gpt-6-sol", "own", 0)
	state, err := s.Set(ctx, "alice", "alice", own)
	require.NoError(t, err)
	assert.EqualValues(t, 1, state.Version)
	replay, err := s.Set(ctx, "alice", "alice", own)
	require.NoError(t, err)
	assert.Equal(t, state, replay)
	_, err = s.Set(ctx, "alice", "alice", change("gpt-6-astra", "stale", 0))
	requireCode(t, err, "stale_model_settings")
	_, err = s.Set(ctx, "alice", "bob", change("gpt-6-astra", "other", 0))
	requireCode(t, err, "forbidden")
	_, err = s.Set(ctx, "alice", modelsettings.GlobalScope, change("gpt-6-astra", "global", 0))
	requireCode(t, err, "forbidden")
	_, err = s.Set(ctx, "bob", modelsettings.GlobalScope, change("gpt-6-astra", "global", 0))
	require.NoError(t, err)
	selected, err := s.Effective(ctx, "alice")
	require.NoError(t, err)
	assert.Equal(t, "gpt-6-sol", selected.Model)
	require.NoError(t, s.Grant(ctx, "bob", modelsettings.Grant{Owner: "alice", Capability: "own", Enabled: false}))
	selected, err = s.Effective(ctx, "alice")
	require.NoError(t, err)
	assert.Equal(t, "gpt-6-astra", selected.Model)
	_, err = s.Set(ctx, "alice", "alice", own)
	requireCode(t, err, "forbidden")
	require.NoError(t, s.Grant(ctx, "bob", modelsettings.Grant{Owner: "alice", Capability: "others", Enabled: true}))
	_, err = s.Set(ctx, "alice", "alice", change("gpt-6-sol", "self", 1))
	requireCode(t, err, "forbidden")
	_, err = s.Set(ctx, "alice", "visitor", change("gpt-6-sol", "assigned", 0))
	require.NoError(t, err)
	require.NoError(t, s.Grant(ctx, "bob", modelsettings.Grant{Owner: "alice", Capability: "others", Enabled: false}))
	selected, err = s.Effective(ctx, "visitor")
	require.NoError(t, err)
	assert.Equal(t, "gpt-6-sol", selected.Model)
	_, err = s.Set(ctx, "bob", "visitor", modelsettings.Change{Version: 1, OperationKey: "reset"})
	require.NoError(t, err)
	selected, err = s.Effective(ctx, "visitor")
	require.NoError(t, err)
	assert.Equal(t, "gpt-6-astra", selected.Model)
	_, err = db.Exec(ctx, `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	permissions, err := s.Permissions(ctx, "bob")
	require.NoError(t, err)
	assert.False(t, permissions["own"])
	assert.False(t, permissions["global"])
	selected, err = s.Effective(ctx, "visitor")
	require.NoError(t, err)
	assert.Equal(t, "gpt-6-astra", selected.Model)
}

func TestModelSettingsTelegramLocalesAndStaleButtons(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			ctx := t.Context()
			_, err := f.db.Exec(ctx, `UPDATE core.users SET language=$1 WHERE id='bob'`, language)
			require.NoError(t, err)
			handle(t, f.b, message(800, 202, "/model"))
			messages := chatMessages(t, f, 202)
			card := messages[len(messages)-1]
			assert.Contains(t, card.Text, "gpt-6-luna")
			var data string
			for _, row := range card.Markup.Rows {
				for _, button := range row {
					if button.Text == "gpt-6-sol / high" {
						data = button.Data
					}
				}
			}
			require.NotEmpty(t, data)
			selected := telegram.Update{
				ID:       801,
				Callback: &telegram.Callback{ID: "801", From: telegram.User{ID: 202}, Message: card, Data: data},
			}
			handle(t, f.b, selected)
			service := modelsettings.Service{DB: f.db}
			effective, err := service.Effective(ctx, "bob")
			require.NoError(t, err)
			assert.Equal(t, "gpt-6-sol", effective.Model)
			selected.ID = 802
			selected.Callback.ID = "802"
			handle(t, f.b, selected)
			state, err := service.Read(ctx, "bob", "bob")
			require.NoError(t, err)
			assert.EqualValues(t, 1, state.Version)
			handle(t, f.b, message(803, 101, "/model"))
			messages = chatMessages(t, f, 101)
			assert.Empty(t, messages[len(messages)-1].Markup.Rows)
			handle(t, f.b, message(804, 202, "/model reset"))
			effective, err = service.Effective(ctx, "bob")
			require.NoError(t, err)
			assert.Equal(t, modelsettings.DefaultModel, effective.Model)
		})
	}
}
