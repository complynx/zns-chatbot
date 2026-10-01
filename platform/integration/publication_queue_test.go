package integration_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/destination"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type publicationResolver func(context.Context, string) (int64, error)

func (f publicationResolver) ResolveChat(ctx context.Context, alias string) (int64, error) {
	return f(ctx, alias)
}

func TestPublicationQueueAliasesFreezeOneLane(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	settings := queueSettings()
	s := adminmessage.Service{DB: db, Delivery: settings}
	calls := 0
	s.DestinationResolver = publicationResolver(func(ctx context.Context, _ string) (int64, error) {
		calls++
		var locked bool
		// The publication's owner row must be free during provider lookup.
		tx, err := db.Begin(ctx)
		require.NoError(t, err)
		defer func() { _ = tx.Rollback(ctx) }()
		err = tx.QueryRow(ctx, `SELECT true FROM core.admin_messages WHERE key='aliases' FOR UPDATE NOWAIT`).
			Scan(&locked)
		require.NoError(t, err)
		require.True(t, locked)
		return -100123, nil
	})
	preview, err := s.Preview(
		t.Context(),
		"bob",
		"aliases",
		adminmessage.Request{
			Destinations: []adminmessage.Destination{{Chat: "@firstchat", Thread: 1}, {Chat: "@secondchat", Thread: 2}},
			Content:      adminmessage.Content{Text: "synthetic"},
		},
	)
	require.NoError(t, err)
	require.NoError(t, s.Enqueue(t.Context(), "bob", preview.ID))
	require.Equal(t, 2, calls)
	s.DestinationResolver = publicationResolver(func(context.Context, string) (int64, error) {
		t.Fatal("queued retry must not resolve aliases again")
		return 999, nil
	})
	require.NoError(t, s.Enqueue(t.Context(), "bob", preview.ID))
	results, err := s.Results(t.Context(), "bob", preview.ID)
	require.NoError(t, err)
	require.Len(t, results, 2)
	second, found, err := s.PrepareDelivery(t.Context(), results[1].ID)
	require.NoError(t, err)
	require.True(t, found)
	gate, err := s.BeginDelivery(t.Context(), delivery.Attempt{ID: second.ID, Generation: second.Attempt})
	require.NoError(t, err)
	require.False(t, gate.Ready)
	first, found, err := s.PrepareDelivery(t.Context(), results[0].ID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "-100123", first.Destination.Chat)
	gate, err = s.BeginDelivery(t.Context(), delivery.Attempt{ID: first.ID, Generation: first.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`,
		first.ID,
	)
	require.NoError(t, err)
	require.NoError(t, s.RecoverDeliveries(t.Context()))
	require.Empty(t, queueCandidates(t, db))
	require.NoError(
		t,
		s.CompleteDelivery(
			t.Context(),
			adminmessage.Completion{
				ID:      first.ID,
				Attempt: first.Attempt,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 77},
			},
		),
	)
	var uncertainAttempt, resends int64
	require.NoError(
		t,
		db.QueryRow(
			t.Context(),
			`SELECT last_uncertain_attempt,uncertain_resends FROM core.admin_message_deliveries WHERE id=$1`,
			first.ID,
		).Scan(&uncertainAttempt, &resends),
	)
	require.Equal(t, first.Attempt, uncertainAttempt)
	require.Zero(t, resends, "a late known response must not admit another send")
	candidates := queueCandidates(t, db)
	require.Len(t, candidates, 1)
	require.Equal(t, "-100123", candidates[0].Destination.Chat)
	require.NoError(t, s.Cancel(t.Context(), "bob", preview.ID))
	require.Empty(t, queueCandidates(t, db))
}

func TestPublicationQueueAnnouncementBindingAndConfigRetirement(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = queueSettings()
	s.AnnouncementBindings = &destination.Bindings{}
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='@firstchat',thread_id=3`)
	require.NoError(t, err)
	command := bookingCommand("solo", "binding", passbooking.Booking{})
	_, err = s.Execute(t.Context(), "alice", command)
	require.Error(t, err)
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_registration_announcements`).Scan(&count),
	)
	require.Zero(t, count)
	require.NoError(
		t,
		s.RefreshAnnouncementDestinations(
			t.Context(),
			publicationResolver(func(context.Context, string) (int64, error) { return -100123, nil }),
			time.Minute,
		),
	)
	_, err = s.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.NoError(
		t,
		s.RefreshAnnouncementDestinations(
			t.Context(),
			publicationResolver(func(context.Context, string) (int64, error) { return -100999, nil }),
			time.Minute,
		),
	)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "-100123", item.Channel)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='@otherchat'`)
	require.NoError(t, err)
	gate, err := s.BeginRegistrationAnnouncement(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempts})
	require.NoError(t, err)
	require.False(t, gate.Ready)
	for _, candidate := range queueCandidates(t, db) {
		require.NotEqual(t, delivery.Announcement, candidate.Reference.Owner)
	}
	_, found, err = s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.False(t, found)
}

func TestPublicationQueueDoesNotAdoptUnboundAnnouncements(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "unbound-announcement", passbooking.Booking{}))
	require.NoError(t, err)
	s.Delivery = queueSettings()
	_, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.False(t, found)
	require.Empty(t, queueCandidates(t, db))
}

func TestPublicationQueueExactHostPrepare(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	s := configureDeliveryFixture(t, f)
	enqueueSyntheticDelivery(t, s, "exact-host", "101")
	var id int64
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT id FROM core.admin_message_deliveries`).Scan(&id))
	item, found, err := f.b.Host.PrepareAdminMessage(t.Context(), id)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, id, item.ID)
	require.Equal(t, "101", item.Destination.Chat)
	gate, err := f.b.Host.BeginAdminMessage(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(
		t,
		f.b.Host.CompleteAdminMessage(
			t.Context(),
			adminmessage.Completion{
				ID:      item.ID,
				Attempt: item.Attempt,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 71},
			},
		),
	)
	require.NoError(t, f.b.Host.RecoverAdminMessages(t.Context()))
	_, found, err = f.b.Host.PrepareAdminMessage(t.Context(), id)
	require.NoError(t, err)
	require.False(t, found)
}

func TestPublicationQueueRefreshIsolatesEligibleAliases(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = queueSettings()
	s.AnnouncementBindings = &destination.Bindings{}
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='@livechat';
 INSERT INTO core.pass_events SELECT (jsonb_populate_record(NULL::core.pass_events,to_jsonb(e)||jsonb_build_object(
 'id','closed-alias','thread_channel','@closedchat','open_ended',false,
 'starts_at',clock_timestamp()-interval '2 days','finishes_at',clock_timestamp()-interval '1 day'))).* FROM core.pass_events e WHERE id='dance'`)
	require.NoError(t, err)
	calls := []string{}
	resolver := publicationResolver(func(_ context.Context, alias string) (int64, error) {
		calls = append(calls, alias)
		if alias != "@livechat" {
			return 0, errors.New("offline")
		}
		return -100123, nil
	})
	require.NoError(t, s.RefreshAnnouncementDestinations(t.Context(), resolver, time.Minute))
	require.Equal(t, []string{"@livechat"}, calls)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events SELECT (jsonb_populate_record(NULL::core.pass_events,to_jsonb(e)||jsonb_build_object('id','broken-alias','thread_channel','@brokenchat'))).* FROM core.pass_events e WHERE id='dance'`,
	)
	require.NoError(t, err)
	resolver = publicationResolver(func(_ context.Context, alias string) (int64, error) {
		if alias != "@livechat" {
			return 0, errors.New("offline")
		}
		return -100456, nil
	})
	require.Error(t, s.RefreshAnnouncementDestinations(t.Context(), resolver, time.Minute))
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "partial-refresh", passbooking.Booking{}))
	require.NoError(t, err)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "-100456", item.Channel)
	_, err = s.AnnouncementBindings.Lookup("@brokenchat")
	require.ErrorIs(t, err, destination.ErrUnavailable)
}
