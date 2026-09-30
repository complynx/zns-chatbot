package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	deliverypolicy "github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestAdminMessageDurableDelivery(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	request := adminmessage.Request{
		Destinations: []adminmessage.Destination{{Chat: "101"}, {Chat: "101"}, {Chat: "-100123", Thread: 4}},
		Content:      adminmessage.Content{Text: "Synthetic message", ParseMode: "HTML"},
	}
	_, err := service.Preview(t.Context(), "alice", "message", request)
	requireCode(t, err, "forbidden")
	preview, err := service.Preview(t.Context(), "bob", "message", request)
	require.NoError(t, err)
	require.Len(t, preview.Request.Destinations, 2)
	_, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	assert.False(t, found, "preview does not send")
	replay, err := service.Preview(t.Context(), "bob", "message", request)
	require.NoError(t, err)
	assert.Equal(t, preview.ID, replay.ID)
	request.Content.Text = "changed"
	_, err = service.Preview(t.Context(), "bob", "message", request)
	requireCode(t, err, "idempotency_conflict")
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	_, err = service.Results(t.Context(), "alice", preview.ID)
	requireCode(t, err, "not_found")
	requireCode(t, service.Enqueue(t.Context(), "alice", preview.ID), "not_found")
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	service = adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	first, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := service.BeginDelivery(t.Context(), deliverypolicy.Attempt{ID: first.ID, Generation: first.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(t, service.Complete(t.Context(), first.ID, first.Attempt, 51, "", false))
	second, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEqual(t, first.ID, second.ID)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET available_at=now()-interval '1 second',lease_until=now()-interval '1 second' WHERE id=$1`,
		second.ID,
	)
	require.NoError(t, err)
	recovered, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, second.ID, recovered.ID)
	requireCode(
		t,
		service.Complete(t.Context(), second.ID, second.Attempt, 52, "", false),
		"admin_message_stale_attempt",
	)
	gate, err = service.BeginDelivery(
		t.Context(),
		deliverypolicy.Attempt{ID: recovered.ID, Generation: recovered.Attempt},
	)
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(
		t,
		service.Complete(t.Context(), recovered.ID, recovered.Attempt, 0, "synthetic blocked recipient", false),
	)
	results, err := service.Results(t.Context(), "bob", preview.ID)
	require.NoError(t, err)
	require.Len(t, results, 2)
	for _, result := range results {
		switch result.ID {
		case first.ID:
			assert.Equal(t, "sent", result.State)
			assert.EqualValues(t, 51, result.TelegramMessageID)
		case recovered.ID:
			assert.Equal(t, "failed", result.State)
		default:
			t.Fatalf("unexpected delivery result %d", result.ID)
		}
	}
	_, found, err = service.Claim(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
}

func TestAdminMessageCancellationAndRevocation(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	request := adminmessage.Request{
		Destinations: []adminmessage.Destination{{Chat: "101"}},
		Content:      adminmessage.Content{FromChat: 202, FromMessage: 4},
	}
	preview, err := service.Preview(t.Context(), "bob", "forward", request)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	require.NoError(t, service.Cancel(t.Context(), "bob", preview.ID))
	requireCode(t, service.Enqueue(t.Context(), "bob", preview.ID), "admin_message_cancelled")
	preview, err = service.Preview(t.Context(), "bob", "revoked", request)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", preview.ID))
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = service.Results(t.Context(), "bob", preview.ID)
	requireCode(t, err, "forbidden")
	_, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
}

func TestAdminMessageRecipientValidation(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	destinations, err := service.ResolveShortcut(t.Context(), "bob", "$dance:admins")
	require.NoError(t, err)
	assert.Equal(t, []adminmessage.Destination{{Chat: "202"}}, destinations)
	_, err = service.ResolveShortcut(t.Context(), "alice", "$dance:admins")
	requireCode(t, err, "forbidden")
	_, err = service.ResolveShortcut(t.Context(), "bob", "$dance:invalid")
	requireCode(t, err, "admin_message_invalid")
	for _, chat := range []string{"0", "https://example.test", "@bad", "101:5", ""} {
		_, err = service.Preview(
			t.Context(),
			"bob",
			"invalid",
			adminmessage.Request{
				Destinations: []adminmessage.Destination{{Chat: chat}},
				Content:      adminmessage.Content{Text: "test"},
			},
		)
		requireCode(t, err, "admin_message_invalid")
	}
}

func TestAdminMessageContentValidation(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	for _, content := range []adminmessage.Content{
		{}, {Text: "hello", ParseMode: "unsupported"}, {Text: "hello", FromChat: 1, FromMessage: 1},
		{FromChat: 1}, {FromMessage: 1}, {Text: strings.Repeat("😀", 2049)},
	} {
		_, err := service.Preview(t.Context(), "bob", "invalid", adminmessage.Request{
			Destinations: []adminmessage.Destination{{Chat: "101"}}, Content: content,
		})
		requireCode(t, err, "admin_message_invalid")
	}
	result, err := service.Preview(t.Context(), "bob", "unicode", adminmessage.Request{
		Destinations: []adminmessage.Destination{{Chat: "@PublicChat"}, {Chat: "@publicchat"}},
		Content:      adminmessage.Content{Text: strings.Repeat("😀", 2048)},
	})
	require.NoError(t, err)
	assert.Equal(t, []adminmessage.Destination{{Chat: "@publicchat"}}, result.Request.Destinations)
}

func TestAdminMessageShortcutCategoriesExcludeCancelled(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	_, err := db.Exec(t.Context(), `
INSERT INTO core.users(id,telegram_id,name) VALUES
 ('msg-paid',1001,'Paid'),('msg-assigned',1002,'Assigned'),
 ('msg-waitlist',1003,'Waiting'),('msg-waiting-for-couple',1004,'Pending'),
 ('msg-cancelled',1005,'Cancelled');
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,
 invitation_target,assigned_at,price)
 SELECT 'dance',id,1,substring(id FROM 5),'leader','solo','bob',now(),
 CASE WHEN id='msg-waiting-for-couple' THEN 202 ELSE 0 END,
 CASE WHEN id IN ('msg-paid','msg-assigned') THEN now() END,
 CASE WHEN id IN ('msg-paid','msg-assigned') THEN 100 END
 FROM core.users WHERE id LIKE 'msg-%';`)
	require.NoError(t, err)
	for _, test := range []struct {
		shortcut string
		chats    []string
	}{
		{"$dance", []string{"1001", "1002"}},
		{"$dance:paid", []string{"1001"}},
		{"$dance:assigned", []string{"1001", "1002"}},
		{"$dance:unpaid", []string{"1002"}},
		{"$dance:waitlist", []string{"1003", "1004"}},
		{"$dance:all", []string{"1001", "1002", "1003", "1004"}},
		{"$dance:admins", []string{"202"}},
	} {
		t.Run(test.shortcut, func(t *testing.T) {
			t.Parallel()
			destinations, resolveErr := service.ResolveShortcut(t.Context(), "bob", test.shortcut)
			require.NoError(t, resolveErr)
			chats := make([]string, 0, len(destinations))
			for _, destination := range destinations {
				chats = append(chats, destination.Chat)
			}
			assert.Equal(t, test.chats, chats)
		})
	}
}
