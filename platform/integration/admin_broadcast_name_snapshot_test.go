package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func TestAdminBroadcastNameSnapshotConcurrentOverride(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"Alice"}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	service := adminmessage.Service{
		DB: db,
		InformalName: func(ctx context.Context, names map[string]any) (string, error) {
			assert.Equal(t, "Alice", names["first_name"])
			_, writeErr := db.Exec(
				ctx,
				`UPDATE core.admin_broadcast_profiles SET overrides='{"informal_name":"ExplicitWinner"}' WHERE owner='alice'`,
			)
			return "GeneratedName", writeErr
		},
	}
	preview, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"name-race",
		`/send_message_to 101 --template --msg '{{.user_informal_name}}'`,
	)
	require.NoError(t, err)
	page, err := service.Review(t.Context(), "bob", preview.ID, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "GeneratedName", page.Items[0].Content.Text)
	var current string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT (fields||overrides)->>'informal_name' FROM core.admin_broadcast_profiles WHERE owner='alice'`).
			Scan(&current),
	)
	assert.Equal(t, "ExplicitWinner", current)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	delivery, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "GeneratedName", delivery.Content.Text)
}

func TestAdminBroadcastNameSnapshotBeforeRecipientRender(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"Alice"}'),('bob','{"first_name":"Bob"}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	calls := 0
	service := adminmessage.Service{
		DB: db,
		InformalName: func(ctx context.Context, names map[string]any) (string, error) {
			calls++
			_, writeErr := db.Exec(
				ctx,
				`UPDATE core.admin_broadcast_profiles SET overrides='{"informal_name":"LaterEdit"}' WHERE owner='bob'`,
			)
			name, ok := names["first_name"].(string)
			assert.True(t, ok)
			return "Generated" + name, writeErr
		},
	}
	preview, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"recipient-race",
		`/send_message_to '[101,202]' --template --msg '{{.user_informal_name}}'`,
	)
	require.NoError(t, err)
	page, err := service.Review(t.Context(), "bob", preview.ID, 0)
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	assert.Equal(t, 2, calls)
	assert.Equal(t, "GeneratedAlice", page.Items[0].Content.Text)
	assert.Equal(t, "GeneratedBob", page.Items[1].Content.Text)
}
