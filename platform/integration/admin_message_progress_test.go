package integration_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestAdminMessageWholeJobProgressPagingAndDurability(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	request := adminmessage.Request{Content: adminmessage.Content{Text: "private broadcast content"}}
	for chat := 1; chat <= 30; chat++ {
		request.Destinations = append(request.Destinations, adminmessage.Destination{Chat: strconv.Itoa(chat)})
	}
	message, err := service.Preview(t.Context(), "bob", "progress", request)
	require.NoError(t, err)
	draft, err := service.Review(t.Context(), "bob", message.ID, 0)
	require.NoError(t, err)
	require.Equal(t, int64(30), draft.Progress.NotQueued)
	require.Zero(t, draft.Progress.Succeeded)
	_, err = db.Exec(t.Context(), `UPDATE core.admin_messages SET state='preparing' WHERE id=$1`, message.ID)
	require.NoError(t, err)
	preparing, err := service.Review(t.Context(), "bob", message.ID, 0)
	require.NoError(t, err)
	require.Equal(t, draft.Progress, preparing.Progress)
	_, err = db.Exec(t.Context(), `UPDATE core.admin_messages SET state='draft' WHERE id=$1`, message.ID)
	require.NoError(t, err)
	require.NoError(t, service.Enqueue(t.Context(), "bob", message.ID))
	_, err = db.Exec(t.Context(), `UPDATE core.admin_message_deliveries SET state=CASE destination->>'chat'
 WHEN '1' THEN 'sent' WHEN '3' THEN 'failed' WHEN '4' THEN 'unknown' WHEN '5' THEN 'sending'
 WHEN '6' THEN 'parked' WHEN '7' THEN 'paused' WHEN '8' THEN 'cancelled' ELSE 'pending' END,
 failure=CASE WHEN destination->>'chat'='2' THEN 'telegram_rate_limit' ELSE '' END,
 available_at=statement_timestamp()-interval '1 minute' WHERE message_id=$1`, message.ID)
	require.NoError(t, err)
	first, err := service.Review(t.Context(), "bob", message.ID, 0)
	require.NoError(t, err)
	second, err := service.Review(t.Context(), "bob", message.ID, 20)
	require.NoError(t, err)
	require.Len(t, first.Items, 20)
	require.Len(t, second.Items, 10)
	require.Equal(t, first.Progress, second.Progress)
	want := adminmessage.JobProgress{
		Total:     30,
		Queued:    22,
		Deferred:  1,
		Sending:   1,
		Succeeded: 1,
		Rejected:  1,
		Cancelled: 1,
		Uncertain: 1,
		Parked:    1,
		Paused:    1,
	}
	require.Equal(t, want, first.Progress)
	// A new service reads durable progress rather than an in-memory counter.
	reconstructed := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	page, err := reconstructed.Review(t.Context(), "bob", message.ID, 20)
	require.NoError(t, err)
	require.Equal(t, want, page.Progress)
	_, err = service.Review(t.Context(), "alice", message.ID, 0)
	requireCode(t, err, "not_found")
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
	require.NoError(t, err)
	_, err = service.Review(t.Context(), "bob", message.ID, 0)
	requireCode(t, err, "forbidden")
}

func TestAdminMessageCancelledDraftProgress(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{Delivery: syntheticDeliverySettings(), DB: db}
	message, err := service.Preview(t.Context(), "bob", "cancelled-progress", adminmessage.Request{
		Destinations: []adminmessage.Destination{
			{Chat: "101"},
			{Chat: "202"},
		},
		Content: adminmessage.Content{Text: "draft"},
	})
	require.NoError(t, err)
	require.NoError(t, service.Cancel(t.Context(), "bob", message.ID))
	page, err := service.Review(t.Context(), "bob", message.ID, 0)
	require.NoError(t, err)
	require.Equal(t, adminmessage.JobProgress{Total: 2, Cancelled: 2}, page.Progress)
}

type progressAliasResolver struct{}

func (progressAliasResolver) ResolveChat(_ context.Context, _ string) (int64, error) {
	return 101, nil
}

func TestAdminMessageProgressSharedPauseAndRecovery(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	service := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings(),
		DestinationResolver: progressAliasResolver{}}
	enqueue := func(s adminmessage.Service, key string, chats ...string) int64 {
		t.Helper()
		request := adminmessage.Request{Content: adminmessage.Content{Text: "synthetic"}}
		for _, chat := range chats {
			request.Destinations = append(request.Destinations, adminmessage.Destination{Chat: chat})
		}
		message, err := s.Preview(t.Context(), "bob", key, request)
		require.NoError(t, err)
		require.NoError(t, s.Enqueue(t.Context(), "bob", message.ID))
		return message.ID
	}
	review := func(id int64, want adminmessage.JobProgress) {
		t.Helper()
		page, err := service.Review(t.Context(), "bob", id, 0)
		require.NoError(t, err)
		require.Equal(t, want, page.Progress)
	}
	first := enqueue(service, "pause-origin", "101")
	item, found, err := service.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, first, item.MessageID)
	gate, err := service.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	// A separate job creates the pause through the actual completion/pacing path.
	outcome := telegram.DeliveryOutcome(0, &telegram.APIError{Code: http.StatusUnauthorized})
	require.Equal(t, delivery.Paused, outcome.Kind)
	require.NoError(t, service.CompleteDelivery(t.Context(), adminmessage.Completion{
		ID: item.ID, Attempt: item.Attempt, Outcome: outcome,
	}))
	job := enqueue(service, "pause-observed", "@synthetic_progress", "202")
	otherChat := enqueue(service, "pause-chat-control", "303")
	otherBotService := service
	otherBotService.Delivery.BotID++
	otherBot := enqueue(otherBotService, "pause-bot-control", "101", "202")
	review(job, adminmessage.JobProgress{Total: 2, Queued: 2, SharedPaused: 2})
	review(otherChat, adminmessage.JobProgress{Total: 1, Queued: 1, SharedPaused: 1})
	review(otherBot, adminmessage.JobProgress{Total: 2, Queued: 2})
	review(first, adminmessage.JobProgress{Total: 1, Paused: 1})
	// Shared pause overlaps a deferred attempt without changing its retry record.
	_, err = db.Exec(t.Context(), `UPDATE core.admin_message_deliveries
 SET failure='telegram_rate_limit',attempt=3,failure_count=2,available_at=statement_timestamp()-interval '1 minute'
 WHERE message_id=$1 AND destination->>'chat'='202'`, job)
	require.NoError(t, err)
	review(job, adminmessage.JobProgress{Total: 2, Queued: 1, Deferred: 1, SharedPaused: 2})
	var attempt, failures int64
	require.NoError(t, db.QueryRow(t.Context(), `SELECT attempt,failure_count FROM core.admin_message_deliveries
 WHERE message_id=$1 AND destination->>'chat'='202'`, job).Scan(&attempt, &failures))
	require.Equal(t, int64(3), attempt)
	require.Equal(t, int64(2), failures)
	_, err = db.Exec(t.Context(), `UPDATE core.admin_message_deliveries SET failure='',attempt=0,failure_count=0
 WHERE message_id=$1 AND destination->>'chat'='202'`, job)
	require.NoError(t, err)
	assertUntouched := func() {
		t.Helper()
		var count int64
		require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.admin_message_deliveries
 WHERE message_id=$1 AND state='pending' AND failure='' AND attempt=0 AND failure_count=0`, job).Scan(&count))
		require.Equal(t, int64(2), count)
	}
	assertUntouched()
	// Synthetic recovery of the bot scope retains the originating chat pause.
	// Alias matching must use the saved queue chat, not the preview's alias.
	_, err = db.Exec(t.Context(), `UPDATE core.delivery_pacing SET pause_reason='',not_before='-infinity'
 WHERE bot_id=$1 AND chat=''`, service.Delivery.BotID)
	require.NoError(t, err)
	review(job, adminmessage.JobProgress{Total: 2, Queued: 2, SharedPaused: 1})
	review(otherChat, adminmessage.JobProgress{Total: 1, Queued: 1})
	review(otherBot, adminmessage.JobProgress{Total: 2, Queued: 2})
	assertUntouched()
	// A cooldown without a saved pause is not a shared service pause.
	_, err = db.Exec(t.Context(), `UPDATE core.delivery_pacing SET pause_reason='',not_before='infinity'
 WHERE bot_id=$1 AND chat='101'`, service.Delivery.BotID)
	require.NoError(t, err)
	review(job, adminmessage.JobProgress{Total: 2, Queued: 2})
	assertUntouched()
	// Review stays observational: terminal domain rows are not reclassified.
	_, err = db.Exec(t.Context(), `UPDATE core.delivery_pacing SET pause_reason='synthetic-private-reason'
 WHERE bot_id=$1 AND chat=''`, service.Delivery.BotID)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.admin_message_deliveries SET state='sent'
 WHERE message_id=$1 AND destination->>'chat'='202'`, job)
	require.NoError(t, err)
	review(job, adminmessage.JobProgress{Total: 2, Queued: 1, Succeeded: 1, SharedPaused: 1})
	_, err = service.Review(t.Context(), "alice", job, 0)
	requireCode(t, err, "not_found")
}
