package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func TestAdminBroadcastResultsPreserveDeliveryContent(t *testing.T) {
	t.Parallel()
	for _, content := range []adminmessage.Content{
		{Text: "<b>Saved text</b>", ParseMode: "HTML"},
		{FromChat: 202, FromMessage: 42},
	} {
		t.Run(content.Text, func(t *testing.T) {
			t.Parallel()
			db, _ := bookingFixture(t)
			service := adminmessage.Service{DB: db}
			preview, err := service.Preview(t.Context(), "bob", "results", adminmessage.Request{
				Destinations: []adminmessage.Destination{{Chat: "101"}}, Content: content,
			})
			require.NoError(t, err)
			require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
			for _, legacy := range []bool{false, true} {
				if legacy {
					_, err = db.Exec(
						t.Context(),
						`UPDATE core.admin_message_deliveries SET content=NULL WHERE message_id=$1`,
						preview.ID,
					)
					require.NoError(t, err)
				}
				results, resultErr := service.Results(t.Context(), "bob", preview.ID)
				require.NoError(t, resultErr)
				require.Len(t, results, 1)
				assert.Equal(t, content, results[0].Content)
			}
			delivery, found, err := service.Claim(t.Context())
			require.NoError(t, err)
			require.True(t, found)
			require.NoError(t, service.Complete(t.Context(), delivery.ID, delivery.Attempt, 88, "", false))
			results, err := service.Results(t.Context(), "bob", preview.ID)
			require.NoError(t, err)
			require.Len(t, results, 1)
			assert.Equal(t, delivery.Content, results[0].Content)
			assert.Equal(t, "sent", results[0].State)
			_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
			require.NoError(t, err)
			_, err = service.Results(t.Context(), "bob", preview.ID)
			require.Error(t, err)
		})
	}
}

func TestAdminBroadcastResultsUsePersonalizedSnapshot(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.admin_broadcast_profiles(owner,fields) VALUES('alice','{"first_name":"Frozen","informal_name":"Frozen"}') ON CONFLICT(owner) DO UPDATE SET fields=EXCLUDED.fields,overrides='{}'`,
	)
	require.NoError(t, err)
	service := adminmessage.Service{DB: db}
	preview, err := service.PreviewCommand(
		t.Context(),
		"bob",
		"personalized-result",
		`/send_message_to 101 --template --msg 'Hello {{.first_name}}'`,
	)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_broadcast_profiles SET overrides='{"first_name":"Later"}' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	results, err := service.Results(t.Context(), "bob", preview.ID)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "Hello Frozen", results[0].Content.Text)
}
