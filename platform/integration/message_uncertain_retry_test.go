package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/store"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type uncertainRateLimitFixture struct {
	db       *pgxpool.Pool
	settings delivery.Settings
	table    string
	chat     string
	send     func() error
	follower func()
}

func TestUncertainMissingRateLimitRetainsProviderFallback(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"admin", "announcement"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					connection, _, err := http.NewResponseController(w).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = connection.Close()
					return
				}
				_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"retry without deadline"}`))
			}))
			t.Cleanup(server.Close)
			client := telegram.Client{Base: server.URL, Token: "synthetic"}
			f := setupUncertainRateLimit(t, owner, client)
			require.NoError(t, f.send())
			require.EqualValues(t, 1, calls.Load(), "the original request really crossed the wire")
			var originalDelay float64
			require.NoError(t, f.db.QueryRow(t.Context(),
				fmt.Sprintf(`SELECT EXTRACT(EPOCH FROM available_at-clock_timestamp()) FROM core.%s`, f.table)).
				Scan(&originalDelay))
			assert.InDelta(t, 5, originalDelay, 2, "uncertainty must use its independent default base")
			_, err := f.db.Exec(
				t.Context(),
				fmt.Sprintf(
					`UPDATE core.%s SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
					f.table,
				),
			)
			require.NoError(t, err)
			require.NoError(t, f.send())
			require.EqualValues(
				t,
				2,
				calls.Load(),
				"the first admitted resend received the confirmed missing-delay 429",
			)
			var state, reason string
			var resends, marker int64
			var delay float64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), fmt.Sprintf(`SELECT state,failure,uncertain_resends,last_uncertain_attempt,EXTRACT(EPOCH FROM available_at-clock_timestamp()) FROM core.%s`, f.table)).
					Scan(&state, &reason, &resends, &marker, &delay),
			)
			assert.Equal(t, "pending", state)
			assert.Equal(t, "telegram_rate_limit", reason)
			assert.EqualValues(t, 1, resends)
			assert.EqualValues(t, 1, marker, "confirmed 429 must not manufacture another uncertainty")
			assert.InDelta(t, 30, delay, 2, "the provider fallback must dominate the ten-second resend backoff")
			for _, chat := range []string{"", f.chat} {
				var cooldown float64
				require.NoError(t, f.db.QueryRow(
					t.Context(),
					`SELECT EXTRACT(EPOCH FROM not_before-clock_timestamp()) FROM core.delivery_pacing WHERE bot_id=$1 AND chat=$2`,
					f.settings.BotID,
					chat,
				).Scan(&cooldown))
				assert.InDelta(t, 30, cooldown, 2, "bot-wide and chat cooldowns must retain the provider fallback")
			}
			f.follower()
			require.Empty(
				t,
				messageRetryCandidates(t, f.db, f.settings.BotID),
				"provider cooldown must block early followers",
			)
			require.NoError(t, f.send())
			assert.EqualValues(t, 2, calls.Load(), "polling must not send before the retained provider cooldown")
		})
	}
}

func setupUncertainRateLimit(t *testing.T, owner string, client telegram.Client) uncertainRateLimitFixture {
	t.Helper()
	if owner == "admin" {
		f := passMenuFixture(t)
		s := configureDeliveryFixture(t, f)
		enqueueSyntheticDelivery(t, s, "missing-cooldown", "101")
		f.b.TG = client
		return uncertainRateLimitFixture{db: f.db, settings: s.Delivery, table: "admin_message_deliveries", chat: "101",
			send:     func() error { return f.b.DeliverAdminMessages(t.Context()) },
			follower: func() { enqueueSyntheticDelivery(t, s, "cooldown-follower", "202") }}
	}
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "missing-cooldown", passbooking.Booking{}))
	require.NoError(t, err)
	return uncertainRateLimitFixture{
		db:       db,
		settings: s.Delivery,
		table:    "pass_registration_announcements",
		chat:     "-100123",
		send: func() error {
			item, found, claimErr := s.ClaimRegistrationAnnouncement(t.Context())
			if claimErr != nil || !found {
				return claimErr
			}
			gate, beginErr := s.BeginRegistrationAnnouncement(
				t.Context(),
				delivery.Attempt{ID: item.ID, Generation: item.Attempts},
			)
			if beginErr != nil || !gate.Ready {
				return beginErr
			}
			var reply telegram.Message
			sendErr := client.Call(t.Context(), "sendMessage", map[string]any{
				"chat_id": int64(-100123), "text": item.Text, "parse_mode": "HTML",
			}, &reply)
			outcome := telegram.DeliveryOutcome(reply.ID, sendErr)
			if outcome.Kind == delivery.Deferred {
				assert.True(t, outcome.Missing, "the confirmed HTTP 429 supplied no retry deadline")
			}
			return s.CompleteRegistrationAnnouncement(t.Context(), passbooking.AnnouncementCompletion{
				ID: item.ID, Attempt: item.Attempts, Outcome: outcome,
			})
		},
		follower: func() {
			_, executeErr := s.Execute(
				t.Context(),
				"bob",
				bookingCommand("solo", "cooldown-follower", passbooking.Booking{}),
			)
			require.NoError(t, executeErr)
		},
	}
}

func TestAnnouncementLostResponseRetainsCapturedWireText(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = syntheticDeliverySettings()
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET thread_channel='-100123',thread_id=42,thread_locale='ru'`,
	)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "captured-wire", passbooking.Booking{}))
	require.NoError(t, err)
	requests := make(chan []byte, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Error(readErr)
			return
		}
		requests <- body
		connection, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr != nil {
			t.Error(hijackErr)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "synthetic"}
	var firstWire []byte
	var originalText string
	for sent := range 4 {
		restarted := passbooking.Service{DB: db, Delivery: s.Delivery}
		services := notificationFixtureServices(db, appservices.Options{})
		services.Registration = restarted
		signer := identity.Signer{Key: []byte("synthetic-announcement-capture-key-32")}
		hostServer := httptest.NewServer(api.Handler(services, signer, slog.New(slog.DiscardHandler)))
		t.Cleanup(hostServer.Close)
		host := appclient.Host{Base: hostServer.URL, Signer: signer}
		item, found, claimErr := host.ClaimRegistrationAnnouncement(t.Context())
		require.NoError(t, claimErr)
		require.True(t, found)
		var captured string
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT rendered_text FROM core.pass_registration_announcements WHERE id=$1`, item.ID).
				Scan(&captured),
		)
		require.Equal(t, captured, item.Text, "wire text must be durable before Begin")
		if sent == 0 {
			originalText = captured
		} else {
			require.Equal(t, originalText, captured)
			rerendered, renderErr := passbooking.RegistrationAnnouncementText(item)
			require.NoError(t, renderErr)
			require.NotEqual(t, captured, rerendered, "changed render inputs must not replace the captured wire text")
		}
		gate, beginErr := host.BeginRegistrationAnnouncement(
			t.Context(),
			delivery.Attempt{ID: item.ID, Generation: item.Attempts},
		)
		require.NoError(t, beginErr)
		require.True(t, gate.Ready)
		var reply telegram.Message
		sendErr := client.Call(t.Context(), "sendMessage", map[string]any{
			"chat_id": int64(-100123), "message_thread_id": *item.ThreadID, "text": item.Text, "parse_mode": "HTML",
		}, &reply)
		require.Error(t, sendErr)
		wire := <-requests
		if sent == 0 {
			firstWire = wire
		} else {
			assert.Equal(t, firstWire, wire, "uncertain resends must retain the complete original request")
		}
		require.NoError(t, host.CompleteRegistrationAnnouncement(t.Context(), passbooking.AnnouncementCompletion{
			ID: item.ID, Attempt: item.Attempts, Outcome: telegram.DeliveryOutcome(reply.ID, sendErr),
		}))
		var state string
		var resends int64
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT state,uncertain_resends,rendered_text FROM core.pass_registration_announcements WHERE id=$1`, item.ID).
				Scan(&state, &resends, &captured),
		)
		assert.EqualValues(t, sent, resends)
		assert.Equal(t, originalText, captured)
		if sent == 3 {
			require.Equal(t, "failed", state)
		} else {
			require.Equal(t, "pending", state)
		}
		_, found, claimErr = host.ClaimRegistrationAnnouncement(t.Context())
		require.NoError(t, claimErr)
		require.False(t, found, "backoff or terminal state must prevent another wire attempt")
		_, err = db.Exec(
			t.Context(),
			`UPDATE core.users SET name='Changed source'; UPDATE core.pass_events SET thread_locale='en'; UPDATE core.pass_bookings SET role='leader';
 UPDATE core.pass_registration_announcements SET name='Changed render input',locale='en',role='leader',available_at=clock_timestamp()-interval '1 second';
 UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
		)
		require.NoError(t, err)
	}
	require.Empty(t, requests)
}

func TestAnnouncementHistoricalUnknownCapturesNextCandidate(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "historical-wire", passbooking.Booking{}))
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET state='unknown',failure='telegram_outcome_unknown'; UPDATE core.delivery_queue SET state='unknown' WHERE owner_kind='announcement'`,
	)
	require.NoError(t, err)
	require.NoError(t, s.RecoverRegistrationAnnouncements(t.Context()))
	var missing bool
	var marker int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT rendered_text IS NULL,last_uncertain_attempt FROM core.pass_registration_announcements`).
			Scan(&missing, &marker),
	)
	require.True(t, missing, "recovery cannot reconstruct historical wire text")
	require.Zero(t, marker)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	expected, err := passbooking.RegistrationAnnouncementText(item)
	require.NoError(t, err)
	assert.Equal(t, expected, item.Text)
	var captured string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT rendered_text,last_uncertain_attempt FROM core.pass_registration_announcements`).
			Scan(&captured, &marker),
	)
	assert.Equal(t, item.Text, captured, "capture describes the next candidate, not the old unknown wire")
	assert.Zero(t, marker)
}

func TestAdminLostResponseResendsWithDurableBudget(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	s := configureDeliveryFixture(t, f)
	enqueueSyntheticDelivery(t, s, "lost-response", "101")
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		connection, _, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)
	f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
	for sent := range 4 {
		require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
		var state string
		require.NoError(t, f.db.QueryRow(t.Context(), `SELECT state FROM core.admin_message_deliveries`).Scan(&state))
		if sent < 3 {
			require.Equal(t, "pending", state, "lost response must schedule a bounded resend")
		} else {
			require.Equal(t, "failed", state)
		}
		var resends, uncertainAttempt int64
		var reason string
		var delay float64
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT uncertain_resends,last_uncertain_attempt,last_uncertain_reason,EXTRACT(EPOCH FROM available_at-clock_timestamp()) FROM core.admin_message_deliveries`).
				Scan(&resends, &uncertainAttempt, &reason, &delay),
		)
		assert.EqualValues(t, sent, resends)
		assert.EqualValues(t, sent+1, uncertainAttempt)
		assert.Equal(t, "telegram_outcome_unknown", reason)
		if sent < 3 {
			assert.InDelta(t, 5*(1<<sent), delay, 2)
		}
		restarted := adminmessage.Service{DB: f.db, Delivery: s.Delivery}
		require.NoError(t, restarted.RecoverDeliveries(t.Context()))
		require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
		assert.EqualValues(t, sent+1, calls.Load(), "backoff must prevent rapid resends")
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
		)
		require.NoError(t, err)
	}
	require.NoError(t, s.RecoverDeliveries(t.Context()))
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	assert.EqualValues(t, 4, calls.Load())
	var failure string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT failure FROM core.admin_message_deliveries`).Scan(&failure))
	assert.Equal(t, "telegram_uncertain_retry_exhausted", failure)
	enqueueSyntheticDelivery(t, s, "follower", "101")
	require.Len(t, messageRetryCandidates(t, f.db, s.Delivery.BotID), 1, "exhaustion must release the next message")
}

func messageRetryCandidates(t *testing.T, db *pgxpool.Pool, botID int64) []delivery.Entry {
	t.Helper()
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	entries, err := delivery.Candidates(t.Context(), tx, botID, 20)
	require.NoError(t, err)
	return entries
}

func TestAdminUncertainRetryUsesPublicationIdentity(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	preview, err := s.Preview(t.Context(), "bob", "two-recipients", adminmessage.Request{
		Destinations: []adminmessage.Destination{{Chat: "101"}, {Chat: "202"}},
		Content:      adminmessage.Content{Text: "synthetic"},
	})
	require.NoError(t, err)
	require.NoError(t, s.Enqueue(t.Context(), "bob", preview.ID))
	results, err := s.Results(t.Context(), "bob", preview.ID)
	require.NoError(t, err)
	require.Len(t, results, 2)
	item, found, err := s.PrepareDelivery(t.Context(), results[1].ID)
	require.NoError(t, err)
	require.True(t, found)
	require.NotEqual(t, preview.ID, item.ID)
	gate, err := s.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(t, s.CompleteDelivery(t.Context(), adminmessage.Completion{
		ID: item.ID, Attempt: item.Attempt,
		Outcome: delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"},
	}))
	var state string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT state FROM core.admin_message_deliveries WHERE id=$1`, item.ID).
		Scan(&state))
	assert.Equal(t, "pending", state)
}

func TestAdminUncertainRecoveryPreservesEvidenceAndPrewireBudget(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	enqueueSyntheticDelivery(t, s, "recover", "101")
	item, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := s.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET lease_until=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	require.NoError(t, s.RecoverDeliveries(t.Context()))
	var firstRecorded time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT last_uncertain_recorded_at FROM core.admin_message_deliveries`).
			Scan(&firstRecorded),
	)
	require.NoError(t, s.RecoverDeliveries(t.Context()))
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	prepared, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	require.Error(
		t,
		s.CompleteDelivery(
			t.Context(),
			adminmessage.Completion{
				ID:      item.ID,
				Attempt: item.Attempt,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 99},
			},
		),
	)
	require.NoError(
		t,
		s.CompleteDelivery(
			t.Context(),
			adminmessage.Completion{
				ID:      prepared.ID,
				Attempt: prepared.Attempt,
				Outcome: delivery.Outcome{Kind: delivery.Deferred, Reason: "admin_identity_unavailable", Missing: true},
			},
		),
	)
	var resends int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT uncertain_resends FROM core.admin_message_deliveries`).Scan(&resends),
	)
	assert.Zero(t, resends, "prewire authorization failure must not consume a resend")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	retry, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	_, err = db.Exec(t.Context(), `UPDATE core.delivery_pacing SET not_before=clock_timestamp()+interval '1 minute'`)
	require.NoError(t, err)
	gate, err = s.BeginDelivery(t.Context(), delivery.Attempt{ID: retry.ID, Generation: retry.Attempt})
	require.NoError(t, err)
	require.False(t, gate.Ready)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT uncertain_resends FROM core.admin_message_deliveries`).Scan(&resends),
	)
	assert.Zero(t, resends, "prewire pacing refusal must not consume a resend")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	retry, found, err = s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err = s.BeginDelivery(t.Context(), delivery.Attempt{ID: retry.ID, Generation: retry.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(
		t,
		s.CompleteDelivery(
			t.Context(),
			adminmessage.Completion{
				ID:      retry.ID,
				Attempt: retry.Attempt,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 45},
			},
		),
	)
	var state string
	var recorded time.Time
	var attempt int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state,uncertain_resends,last_uncertain_attempt,last_uncertain_recorded_at FROM core.admin_message_deliveries`).
			Scan(&state, &resends, &attempt, &recorded),
	)
	assert.Equal(t, "sent", state)
	assert.EqualValues(t, 1, resends)
	assert.Equal(t, item.Attempt, attempt)
	assert.Equal(t, firstRecorded, recorded)
}

func TestLegacyAdminUnknownIsObservedAndCancelledWithoutResend(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	enqueueSyntheticDelivery(t, s, "legacy-unknown", "101")
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET state='unknown',failure='telegram_outcome_unknown'; UPDATE core.delivery_queue SET state='unknown'; UPDATE core.admin_messages SET state='cancelled'`,
	)
	require.NoError(t, err)
	require.NoError(t, s.RecoverDeliveries(t.Context()))
	var state, reason string
	var attempt, resends int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state,last_uncertain_reason,last_uncertain_attempt,uncertain_resends FROM core.admin_message_deliveries`).
			Scan(&state, &reason, &attempt, &resends),
	)
	assert.Equal(t, "cancelled", state)
	assert.Equal(t, "telegram_outcome_unknown", reason)
	assert.Zero(t, attempt, "legacy zero generation must not become invented history")
	assert.Zero(t, resends)
	require.NoError(t, s.RecoverDeliveries(t.Context()))
	require.Empty(t, messageRetryCandidates(t, db, s.Delivery.BotID))
}

func TestLegacyAdminInvalidCaptureNeverReachesTransport(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		capture string
		retry   bool
	}{{"{}", false}, {"null", false}, {"{}", true}, {"null", true}, {"SQL NULL", true}} {
		t.Run(fmt.Sprintf("%s/retry=%t", scenario.capture, scenario.retry), func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			s := configureDeliveryFixture(t, f)
			enqueueSyntheticDelivery(t, s, "legacy-invalid-capture", "101")
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.admin_message_deliveries SET content='{"text":"Personalized original","parse_mode":"HTML"}'::jsonb`,
			)
			require.NoError(t, err)
			var calls atomic.Int64
			requests := make(chan []byte, 4)
			server := httptest.NewServer(adminCaptureReply(t, requests, &calls, scenario.retry))
			t.Cleanup(server.Close)
			f.b.TG = telegram.Client{Base: server.URL, Token: "synthetic"}
			if scenario.retry {
				require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
				require.EqualValues(t, 1, calls.Load(), "original admitted request crossed HTTP and lost its reply")
				var original map[string]any
				require.NoError(t, json.Unmarshal(<-requests, &original))
				assert.Equal(t, "Personalized original", original["text"])
				assert.Equal(t, "HTML", original["parse_mode"])
			}
			transportBefore := calls.Load()
			var capture any = scenario.capture
			if scenario.capture == "SQL NULL" {
				capture = nil
			}
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.admin_message_deliveries SET state='unknown',content=$1::jsonb,failure='telegram_outcome_unknown'`,
				capture,
			)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.delivery_queue SET state='unknown'`)
			require.NoError(t, err)
			require.NoError(t, s.RecoverDeliveries(t.Context()))
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'`,
			)
			require.NoError(t, err)
			var before string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT to_jsonb(d)::text FROM core.admin_message_deliveries d`).
					Scan(&before),
			)
			err = f.b.DeliverAdminMessages(t.Context())
			require.ErrorContains(t, err, "admin_message_original_wire_unavailable")
			assert.Equal(
				t,
				transportBefore,
				calls.Load(),
				"invalid durable capture cannot admit an empty transport request",
			)
			var after string
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT to_jsonb(d)::text FROM core.admin_message_deliveries d`).
					Scan(&after),
			)
			assert.Equal(t, before, after, "capture refusal cannot mutate lease, attempt, receipt or retry budget")
		})
	}
}

func adminCaptureReply(t *testing.T, requests chan<- []byte, calls *atomic.Int64, retry bool) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Error(readErr)
			return
		}
		requests <- body
		if calls.Add(1) == 1 && retry {
			connection, _, hijackErr := http.NewResponseController(w).Hijack()
			if hijackErr != nil {
				t.Error(hijackErr)
				return
			}
			_ = connection.Close()
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":91}}`))
	}
}

// Restore the predecessor schema only in this test's disposable database.
func restoreAdminCapturePredecessor(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	_, err := db.Exec(t.Context(), `ALTER TABLE core.admin_message_deliveries
 DROP COLUMN IF EXISTS content_captured,
 DROP COLUMN last_confirmed_attempt,DROP COLUMN last_uncertain_attempt,
 DROP COLUMN last_uncertain_reason,DROP COLUMN last_uncertain_recorded_at,DROP COLUMN uncertain_resends;
 ALTER TABLE core.pass_registration_announcements
 DROP COLUMN rendered_text,DROP COLUMN rendered_admitted,
 DROP COLUMN last_confirmed_attempt,DROP COLUMN last_uncertain_attempt,
 DROP COLUMN last_uncertain_reason,DROP COLUMN last_uncertain_recorded_at,DROP COLUMN uncertain_resends;
 DELETE FROM public.zns_schema_migrations WHERE name='094_message_uncertain_retry.sql'`)
	require.NoError(t, err)
}

func TestLegacyAdminNullCapturesNextAdmittedWire(t *testing.T) {
	t.Parallel()
	for _, refusal := range []string{"pause", "pacing", "expired-preparation"} {
		t.Run(refusal, func(t *testing.T) {
			t.Parallel()
			checkLegacyAdminNullCapture(t, refusal)
		})
	}
}

func TestAdminCaptureUpgradePreservesPredecessor(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	enqueueSyntheticDelivery(t, s, "historical-null", "101")
	enqueueSyntheticDelivery(t, s, "historical-content", "202")
	restoreAdminCapturePredecessor(t, db)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET content=NULL,state='unknown',failure='telegram_outcome_unknown',attempt=7,failure_count=2,telegram_message_id=91 WHERE destination->>'chat'='101'`,
	)
	require.NoError(t, err)
	var before string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(d) ORDER BY id)::text FROM core.admin_message_deliveries d`).
			Scan(&before),
	)
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Migrate(t.Context(), db), "replay must preserve capture classification")
	var after string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT jsonb_agg(to_jsonb(d)-ARRAY['content_captured','last_confirmed_attempt','last_uncertain_attempt','last_uncertain_reason','last_uncertain_recorded_at','uncertain_resends'] ORDER BY id)::text FROM core.admin_message_deliveries d`).
			Scan(&after),
	)
	assert.JSONEq(t, before, after, "upgrade/replay must preserve every predecessor field")
	var truthful bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT bool_and(content_captured=(content IS NOT NULL) AND last_uncertain_attempt IS NULL AND last_confirmed_attempt IS NULL AND uncertain_resends=0) FROM core.admin_message_deliveries`).
			Scan(&truthful),
	)
	assert.True(t, truthful, "migration classifies custody without inventing admission history")
	enqueueSyntheticDelivery(t, s, "new-captured", "303")
	var captured bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT content_captured FROM core.admin_message_deliveries WHERE destination->>'chat'='303'`).
			Scan(&captured),
	)
	assert.True(t, captured, "new stored snapshots never inherit historical fallback")
}

func checkLegacyAdminNullCapture(t *testing.T, refusal string) {
	t.Helper()
	f := passMenuFixture(t)
	s := configureDeliveryFixture(t, f)
	enqueueSyntheticDelivery(t, s, "legacy-null", "101")
	restoreAdminCapturePredecessor(t, f.db)
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET content=NULL,state='unknown',failure='telegram_outcome_unknown'; UPDATE core.delivery_queue SET state='unknown'`,
	)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), f.db))
	require.NoError(t, s.RecoverDeliveries(t.Context()))
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	first, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	if refusal == "expired-preparation" {
		_, err = f.db.Exec(
			t.Context(),
			`UPDATE core.admin_message_deliveries SET lease_until=clock_timestamp()-interval '1 second'`,
		)
		require.NoError(t, err)
	} else {
		_, err = f.db.Exec(
			t.Context(),
			`INSERT INTO core.delivery_pacing(bot_id,chat) VALUES($1,'') ON CONFLICT DO NOTHING`,
			s.Delivery.BotID,
		)
		require.NoError(t, err)
		query := `UPDATE core.delivery_pacing SET not_before=clock_timestamp()+interval '120 seconds'`
		if refusal == "pause" {
			query = `UPDATE core.delivery_pacing SET pause_reason='synthetic_operator_pause'`
		}
		_, err = f.db.Exec(t.Context(), query)
		require.NoError(t, err)
		gate, beginErr := s.BeginDelivery(t.Context(), delivery.Attempt{ID: first.ID, Generation: first.Attempt})
		require.NoError(t, beginErr)
		require.False(t, gate.Ready)
	}
	var resends int64
	var contentCaptured bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT uncertain_resends,content_captured FROM core.admin_message_deliveries`).
			Scan(&resends, &contentCaptured),
	)
	assert.Zero(t, resends)
	assert.False(t, contentCaptured, "prewire refusal cannot freeze the provisional candidate")
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.admin_messages SET request=jsonb_set(request,'{content}','{"text":"Next admitted candidate","parse_mode":"HTML"}'::jsonb);
 UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second',pause_reason=''`,
	)
	require.NoError(t, err)
	item, found, err := s.Claim(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.NotEqual(t, first.Content, item.Content)
	assert.Equal(t, "Next admitted candidate", item.Content.Text)
	gate, err := s.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	var captured []byte
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content,uncertain_resends,content_captured FROM core.admin_message_deliveries`).
			Scan(&captured, &resends, &contentCaptured),
	)
	var content adminmessage.Content
	require.NoError(t, json.Unmarshal(captured, &content))
	assert.Equal(t, item.Content, content, "the next actual admission durably freezes its returned candidate")
	assert.True(t, contentCaptured)
	assert.EqualValues(t, 1, resends)
	wires := make(chan []byte, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Error(readErr)
			return
		}
		wires <- body
		connection, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr != nil {
			t.Error(hijackErr)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "synthetic"}
	var reply telegram.Message
	sendErr := client.Call(
		t.Context(),
		"sendMessage",
		map[string]any{"chat_id": 101, "text": item.Content.Text, "parse_mode": item.Content.ParseMode},
		&reply,
	)
	require.Error(t, sendErr)
	firstWire := <-wires
	require.NoError(
		t,
		s.CompleteDelivery(
			t.Context(),
			adminmessage.Completion{
				ID:      item.ID,
				Attempt: item.Attempt,
				Outcome: telegram.DeliveryOutcome(reply.ID, sendErr),
			},
		),
	)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.admin_messages SET request=jsonb_set(request,'{content}','{"text":"Different publication"}'::jsonb); UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	f.b.TG = client
	require.NoError(t, f.b.DeliverAdminMessages(t.Context()))
	assert.Equal(t, firstWire, <-wires, "publication changes cannot reconstruct the admitted wire")
}

func TestTerminalPositiveReceiptRejectsContradictoryNegative(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"admin", "announcement"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			f := prepareRecoveredReply(t, owner)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":91}}`))
			}))
			t.Cleanup(server.Close)
			client := telegram.Client{Base: server.URL, Token: "synthetic"}
			var reply telegram.Message
			err := client.Call(t.Context(), "sendMessage", f.payload, &reply)
			require.NoError(t, err)
			positive := telegram.DeliveryOutcome(reply.ID, err)
			require.EqualValues(t, 91, positive.MessageID)
			_, err = f.db.Exec(
				t.Context(),
				`UPDATE core.`+f.table+` SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`,
				f.id,
			)
			require.NoError(t, err)
			require.NoError(t, f.recover())
			require.NoError(t, f.finish(delivery.Outcome{Kind: delivery.Rejected, Reason: "original_wire_unavailable"}))
			require.NoError(
				t,
				f.finish(positive),
				"the real HTTP positive receipt is retained after local terminal policy",
			)
			before := f.snapshot(t)
			// Inject a contradictory callback; the HTTP request above returned only the positive reply.
			require.Error(t, f.finish(delivery.Outcome{Kind: delivery.Rejected, Reason: "telegram_recipient_rejected"}))
			assert.Equal(t, before, f.snapshot(t), "contradictory negative cannot overwrite confirmation history")
			require.NoError(t, f.finish(positive), "identical positive receipt replay remains idempotent")
			assert.Equal(t, before, f.snapshot(t))
			var messageID int64
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT `+f.messageColumn+` FROM core.`+f.table+` WHERE id=$1`, f.id).
					Scan(&messageID),
			)
			assert.EqualValues(t, 91, messageID)
		})
	}
}

func TestAdminLastUncertainResendRateLimitStillPacesOtherMessages(t *testing.T) {
	t.Parallel()
	db, _ := bookingFixture(t)
	s := adminmessage.Service{DB: db, Delivery: syntheticDeliverySettings()}
	enqueueSyntheticDelivery(t, s, "limited-final-resend", "101")
	for admitted := range 4 {
		item, found, err := s.Claim(t.Context())
		require.NoError(t, err)
		require.True(t, found)
		gate, err := s.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
		require.NoError(t, err)
		require.True(t, gate.Ready)
		outcome := delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"}
		if admitted == 3 {
			outcome = delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 120}
		}
		require.NoError(
			t,
			s.CompleteDelivery(
				t.Context(),
				adminmessage.Completion{ID: item.ID, Attempt: item.Attempt, Outcome: outcome},
			),
		)
		if admitted < 3 {
			_, err = db.Exec(
				t.Context(),
				`UPDATE core.admin_message_deliveries SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
			)
			require.NoError(t, err)
		}
	}
	var state string
	var uncertainAttempt, resends int64
	var cooldown float64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT state,last_uncertain_attempt,uncertain_resends FROM core.admin_message_deliveries`).
			Scan(&state, &uncertainAttempt, &resends),
	)
	assert.Equal(t, "failed", state)
	assert.EqualValues(t, 3, resends)
	assert.EqualValues(t, 3, uncertainAttempt, "confirmed 429 must not become another uncertainty")
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT EXTRACT(EPOCH FROM not_before-clock_timestamp()) FROM core.delivery_pacing WHERE chat=''`).
			Scan(&cooldown),
	)
	assert.Greater(t, cooldown, 118.0, "exhaustion must retain the observed provider cooldown")
	enqueueSyntheticDelivery(t, s, "other-chat", "202")
	require.Empty(t, messageRetryCandidates(t, db, s.Delivery.BotID))
}

func TestAnnouncementUnknownResendsAndCrashExhaustsBudget(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	s.Delivery = syntheticDeliverySettings()
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "announcement-retry", passbooking.Booking{}))
	require.NoError(t, err)
	for admitted := range 4 {
		item, found, claimErr := s.ClaimRegistrationAnnouncement(t.Context())
		require.NoError(t, claimErr)
		require.True(t, found)
		if admitted > 0 {
			require.Error(t, s.CompleteRegistrationAnnouncement(t.Context(), passbooking.AnnouncementCompletion{
				ID: item.ID, Attempt: item.Attempts - 1,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 99},
			}))
		}
		gate, beginErr := s.BeginRegistrationAnnouncement(
			t.Context(),
			delivery.Attempt{ID: item.ID, Generation: item.Attempts},
		)
		require.NoError(t, beginErr)
		require.True(t, gate.Ready)
		if admitted == 0 {
			require.NoError(
				t,
				s.CompleteRegistrationAnnouncement(
					t.Context(),
					passbooking.AnnouncementCompletion{
						ID:      item.ID,
						Attempt: item.Attempts,
						Outcome: delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"},
					},
				),
			)
		} else {
			_, err = db.Exec(
				t.Context(),
				`UPDATE core.pass_registration_announcements SET lease_until=clock_timestamp()-interval '1 second'`,
			)
			require.NoError(t, err)
			restarted := s
			require.NoError(t, restarted.RecoverRegistrationAnnouncements(t.Context()))
		}
		var state, reason string
		var resends int64
		require.NoError(
			t,
			db.QueryRow(t.Context(), `SELECT state,failure,uncertain_resends FROM core.pass_registration_announcements`).
				Scan(&state, &reason, &resends),
		)
		assert.EqualValues(t, admitted, resends)
		if admitted < 3 {
			assert.Equal(t, "pending", state)
		} else {
			assert.Equal(t, "failed", state)
			assert.Equal(t, "telegram_uncertain_retry_exhausted", reason)
			require.NoError(t, s.CompleteRegistrationAnnouncement(t.Context(), passbooking.AnnouncementCompletion{
				ID: item.ID, Attempt: item.Attempts,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 99},
			}))
		}
		_, err = db.Exec(
			t.Context(),
			`UPDATE core.pass_registration_announcements SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
		)
		require.NoError(t, err)
	}
	require.NoError(t, s.RecoverRegistrationAnnouncements(t.Context()))
	_, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	assert.False(t, found)
}

func TestLegacyUnknownLateResultsRetainEvidence(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"admin", "announcement", "admin-recovered", "announcement-recovered"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			db, s := bookingFixture(t)
			s.Delivery = syntheticDeliverySettings()
			var state, reason string
			var attempt, resends int64
			if owner == "admin" || owner == "admin-recovered" {
				admin := adminmessage.Service{DB: db, Delivery: s.Delivery}
				enqueueSyntheticDelivery(t, admin, "late-result", "101")
				item, found, err := admin.Claim(t.Context())
				require.NoError(t, err)
				require.True(t, found)
				gate, err := admin.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
				require.NoError(t, err)
				require.True(t, gate.Ready)
				_, err = db.Exec(
					t.Context(),
					`UPDATE core.admin_message_deliveries SET state='unknown',failure='telegram_outcome_unknown'; UPDATE core.delivery_queue SET state='unknown' WHERE owner_kind='admin'`,
				)
				require.NoError(t, err)
				if owner == "admin-recovered" {
					require.NoError(t, admin.RecoverDeliveries(t.Context()))
				}
				require.NoError(
					t,
					admin.CompleteDelivery(
						t.Context(),
						adminmessage.Completion{
							ID:      item.ID,
							Attempt: item.Attempt,
							Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 91},
						},
					),
				)
				require.NoError(
					t,
					db.QueryRow(t.Context(), `SELECT state,last_uncertain_reason,last_uncertain_attempt,uncertain_resends FROM core.admin_message_deliveries`).
						Scan(&state, &reason, &attempt, &resends),
				)
			} else {
				_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
				require.NoError(t, err)
				_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "late-result", passbooking.Booking{}))
				require.NoError(t, err)
				item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
				require.NoError(t, err)
				require.True(t, found)
				gate, err := s.BeginRegistrationAnnouncement(
					t.Context(),
					delivery.Attempt{ID: item.ID, Generation: item.Attempts},
				)
				require.NoError(t, err)
				require.True(t, gate.Ready)
				_, err = db.Exec(
					t.Context(),
					`UPDATE core.pass_registration_announcements SET state='unknown',failure='telegram_outcome_unknown'; UPDATE core.delivery_queue SET state='unknown' WHERE owner_kind='announcement'`,
				)
				require.NoError(t, err)
				if owner == "announcement-recovered" {
					require.NoError(t, s.RecoverRegistrationAnnouncements(t.Context()))
				}
				require.NoError(
					t,
					s.CompleteRegistrationAnnouncement(
						t.Context(),
						passbooking.AnnouncementCompletion{
							ID:      item.ID,
							Attempt: item.Attempts,
							Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: 92},
						},
					),
				)
				require.NoError(
					t,
					db.QueryRow(t.Context(), `SELECT state,last_uncertain_reason,last_uncertain_attempt,uncertain_resends FROM core.pass_registration_announcements`).
						Scan(&state, &reason, &attempt, &resends),
				)
			}
			assert.Equal(t, "sent", state)
			assert.Equal(t, "telegram_outcome_unknown", reason)
			assert.EqualValues(t, 1, attempt)
			assert.Zero(t, resends, "a late known response is not a resend admission")
		})
	}
}

type terminalReceiptFixture struct {
	db                                  *pgxpool.Pool
	table, messageColumn, attemptColumn string
	id                                  int64
	complete                            func(int64) error
}

func newTerminalReceiptFixture(t *testing.T, owner string) terminalReceiptFixture {
	t.Helper()
	db, s := bookingFixture(t)
	s.Delivery = syntheticDeliverySettings()
	f := terminalReceiptFixture{db: db}
	if owner == "admin" {
		admin := adminmessage.Service{DB: db, Delivery: s.Delivery}
		enqueueSyntheticDelivery(t, admin, "terminal-receipt", "101")
		item, found, err := admin.Claim(t.Context())
		require.NoError(t, err)
		require.True(t, found)
		gate, err := admin.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
		require.NoError(t, err)
		require.True(t, gate.Ready)
		require.NoError(t, admin.CompleteDelivery(t.Context(), adminmessage.Completion{
			ID: item.ID, Attempt: item.Attempt,
			Outcome: delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"},
		}))
		f.table, f.messageColumn, f.attemptColumn = "admin_message_deliveries", "telegram_message_id", "attempt"
		f.id = item.ID
		f.complete = func(messageID int64) error {
			return admin.CompleteDelivery(t.Context(), adminmessage.Completion{
				ID:      item.ID,
				Attempt: item.Attempt,
				Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: messageID},
			})
		}
		return f
	}
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "terminal-receipt", passbooking.Booking{}))
	require.NoError(t, err)
	item, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := s.BeginRegistrationAnnouncement(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempts})
	require.NoError(t, err)
	require.True(t, gate.Ready)
	require.NoError(t, s.CompleteRegistrationAnnouncement(t.Context(), passbooking.AnnouncementCompletion{
		ID: item.ID, Attempt: item.Attempts,
		Outcome: delivery.Outcome{Kind: delivery.Uncertain, Reason: "telegram_outcome_unknown"},
	}))
	f.table, f.messageColumn, f.attemptColumn = "pass_registration_announcements", "message_id", "attempts"
	f.id = item.ID
	f.complete = func(messageID int64) error {
		return s.CompleteRegistrationAnnouncement(t.Context(), passbooking.AnnouncementCompletion{
			ID:      item.ID,
			Attempt: item.Attempts,
			Outcome: delivery.Outcome{Kind: delivery.Succeeded, MessageID: messageID},
		})
	}
	return f
}

func (f terminalReceiptFixture) snapshot(t *testing.T) string {
	t.Helper()
	var snapshot string
	err := f.db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'owner',(SELECT to_jsonb(d)-$1::text FROM core.`+f.table+` d WHERE id=$2),
 'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY bot_id,owner_kind,owner_key,effect_key) FROM core.delivery_queue q),
 'lanes',(SELECT jsonb_agg(to_jsonb(l) ORDER BY bot_id,chat) FROM core.delivery_lanes l),
 'fairness',(SELECT jsonb_agg(to_jsonb(f) ORDER BY bot_id) FROM core.delivery_fairness f),
 'pacing',(SELECT jsonb_agg(to_jsonb(p) ORDER BY bot_id,chat) FROM core.delivery_pacing p),
 'messages',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM core.admin_messages m),
 'bookings',(SELECT jsonb_agg(to_jsonb(b) ORDER BY event_id,owner,created_at) FROM core.pass_bookings b),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY id) FROM core.pass_events e))::text`, f.messageColumn, f.id).
		Scan(&snapshot)
	require.NoError(t, err)
	return snapshot
}

func TestTerminalUncertainReceiptsPreservePolicy(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"admin", "announcement"} {
		for _, scenario := range []string{"failed", "cancelled", "contradictory", "stale", "later-429", "no-marker", "live-lease"} {
			t.Run(owner+"/"+scenario, func(t *testing.T) {
				t.Parallel()
				checkTerminalReceipt(t, owner, scenario)
			})
		}
	}
}

func checkTerminalReceipt(t *testing.T, owner, scenario string) {
	t.Helper()
	if scenario == "later-429" {
		checkRecoveredConfirmedReply(t, owner, "cancelled-after-reply")
		return
	}
	f := newTerminalReceiptFixture(t, owner)
	state := "failed"
	if scenario == "cancelled" {
		state = "cancelled"
	}
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.`+f.table+` SET state=$1,lease_until=NULL WHERE id=$2`,
		state,
		f.id,
	)
	require.NoError(t, err)
	_, err = f.db.Exec(t.Context(), `UPDATE core.delivery_queue SET state=$1`, state)
	require.NoError(t, err)
	mutation := ""
	switch scenario {
	case "contradictory":
		mutation = f.messageColumn + "=92"
	case "stale":
		mutation = f.attemptColumn + "=" + f.attemptColumn + "+1"
	case "no-marker":
		mutation = "last_uncertain_attempt=NULL,last_uncertain_reason=NULL,last_uncertain_recorded_at=NULL"
	case "live-lease":
		mutation = "lease_until=clock_timestamp()+interval '1 minute'"
	}
	if mutation != "" {
		_, err = f.db.Exec(t.Context(), `UPDATE core.`+f.table+` SET `+mutation+` WHERE id=$1`, f.id)
		require.NoError(t, err)
	}
	before := f.snapshot(t)
	if scenario == "failed" || scenario == "cancelled" {
		require.NoError(t, f.complete(91))
		require.NoError(t, f.complete(91), "the same factual receipt is idempotent")
		require.Error(t, f.complete(92), "a contradictory receipt must be rejected")
	} else {
		require.Error(t, f.complete(91))
	}
	assert.Equal(
		t,
		before,
		f.snapshot(t),
		"receipt must preserve policy, queue, pacing and source payloads",
	)
	var messageID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT `+f.messageColumn+` FROM core.`+f.table+` WHERE id=$1`, f.id).
			Scan(&messageID),
	)
	switch scenario {
	case "failed", "cancelled":
		assert.EqualValues(t, 91, messageID)
	case "contradictory":
		assert.EqualValues(t, 92, messageID)
	default:
		assert.Zero(t, messageID)
	}
}

type recoveredReplyFixture struct {
	terminalReceiptFixture

	attempt int64
	payload map[string]any
	finish  func(delivery.Outcome) error
	recover func() error
	cancel  func() error
}

func prepareRecoveredReply(t *testing.T, owner string) recoveredReplyFixture {
	t.Helper()
	db, registration := bookingFixture(t)
	registration.Delivery = syntheticDeliverySettings()
	f := recoveredReplyFixture{db: db}
	if owner == "admin" {
		s := adminmessage.Service{DB: db, Delivery: registration.Delivery}
		enqueueSyntheticDelivery(t, s, "confirmed-late-reply", "101")
		item, found, err := s.Claim(t.Context())
		require.NoError(t, err)
		require.True(t, found)
		gate, err := s.BeginDelivery(t.Context(), delivery.Attempt{ID: item.ID, Generation: item.Attempt})
		require.NoError(t, err)
		require.True(t, gate.Ready)
		f.table, f.messageColumn, f.attemptColumn = "admin_message_deliveries", "telegram_message_id", "attempt"
		f.id, f.attempt = item.ID, item.Attempt
		f.payload = map[string]any{"chat_id": 101, "text": item.Content.Text}
		f.finish = func(outcome delivery.Outcome) error {
			return s.CompleteDelivery(
				t.Context(),
				adminmessage.Completion{ID: item.ID, Attempt: item.Attempt, Outcome: outcome},
			)
		}
		f.recover = func() error { return s.RecoverDeliveries(t.Context()) }
		f.cancel = func() error { return s.Cancel(t.Context(), item.Actor, item.MessageID) }
		return f
	}
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	booking, err := registration.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "confirmed-late-reply", passbooking.Booking{}),
	)
	require.NoError(t, err)
	item, found, err := registration.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	gate, err := registration.BeginRegistrationAnnouncement(
		t.Context(),
		delivery.Attempt{ID: item.ID, Generation: item.Attempts},
	)
	require.NoError(t, err)
	require.True(t, gate.Ready)
	f.table, f.messageColumn, f.attemptColumn = "pass_registration_announcements", "message_id", "attempts"
	f.id, f.attempt = item.ID, item.Attempts
	f.payload = map[string]any{"chat_id": -100123, "text": item.Text, "parse_mode": "HTML"}
	f.finish = func(outcome delivery.Outcome) error {
		return registration.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{ID: item.ID, Attempt: item.Attempts, Outcome: outcome},
		)
	}
	f.recover = func() error { return registration.RecoverRegistrationAnnouncements(t.Context()) }
	f.cancel = func() error {
		_, cancelErr := registration.Execute(
			t.Context(),
			"alice",
			bookingCommand("cancel", "cancel-late-reply", booking),
		)
		if cancelErr != nil {
			return cancelErr
		}
		return f.recover()
	}
	return f
}

func TestRecoveredConfirmedReplyFencesContradictorySuccess(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"admin", "announcement"} {
		for _, phase := range []string{"pending", "cancelled-before-reply", "cancelled-after-reply", "prewire-rejection"} {
			t.Run(owner+"/"+phase, func(t *testing.T) {
				t.Parallel()
				checkRecoveredConfirmedReply(t, owner, phase)
			})
		}
	}
}

func TestRecoveredCanonicalNegativeReplayPreservesSchedule(t *testing.T) {
	t.Parallel()
	for _, owner := range []string{"admin", "announcement"} {
		t.Run(owner, func(t *testing.T) {
			t.Parallel()
			checkRecoveredConfirmedReply(t, owner, "negative-replay")
		})
	}
}

func checkRecoveredConfirmedReply(t *testing.T, owner, phase string) {
	t.Helper()
	f := prepareRecoveredReply(t, owner)
	if phase == "negative-replay" {
		chat := "101"
		if owner == "announcement" {
			chat = "-100123"
		}
		enqueueSyntheticDelivery(t, adminmessage.Service{DB: f.db, Delivery: syntheticDeliverySettings()},
			"negative-replay-follower", chat)
	}
	entered := make(chan []byte, 1)
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		entered <- body
		<-release
		if phase == "prewire-rejection" {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":91}}`))
			return
		}
		_, _ = w.Write(
			[]byte(
				`{"ok":false,"error_code":429,"description":"late confirmed rate limit","parameters":{"retry_after":120}}`,
			),
		)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	client := telegram.Client{Base: server.URL, Token: "synthetic"}
	done := make(chan delivery.Outcome, 1)
	go func() {
		var reply telegram.Message
		err := client.Call(t.Context(), "sendMessage", f.payload, &reply)
		done <- telegram.DeliveryOutcome(reply.ID, err)
	}()
	select {
	case body := <-entered:
		require.NotEmpty(t, body, "the exact admitted generation crossed the real HTTP boundary")
	case <-time.After(10 * time.Second):
		t.Fatal("synthetic transport did not observe the admitted request")
	}
	_, err := f.db.Exec(
		t.Context(),
		`UPDATE core.`+f.table+` SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`,
		f.id,
	)
	require.NoError(t, err)
	require.NoError(t, f.recover())
	var originalMarker string
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT jsonb_build_array(last_uncertain_attempt,last_uncertain_reason,last_uncertain_recorded_at)::text FROM core.`+f.table+` WHERE id=$1`, f.id).
			Scan(&originalMarker),
	)
	if phase == "cancelled-before-reply" {
		require.NoError(t, f.cancel())
	}
	if phase == "prewire-rejection" {
		// A local rejection is not a provider observation for the unresolved call.
		require.NoError(
			t,
			f.finish(delivery.Outcome{Kind: delivery.Rejected, Reason: "announcement_original_wire_unavailable"}),
		)
		before := f.snapshot(t)
		var noConfirmedWire bool
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT last_confirmed_attempt IS NULL FROM core.`+f.table+` WHERE id=$1`, f.id).
				Scan(&noConfirmedWire),
		)
		require.True(t, noConfirmedWire, "a local rejection must not manufacture a provider observation")
		once.Do(func() { close(release) })
		select {
		case positive := <-done:
			require.Equal(t, delivery.Succeeded, positive.Kind)
			require.EqualValues(t, 91, positive.MessageID)
			require.NoError(t, f.finish(positive))
		case <-time.After(10 * time.Second):
			t.Fatal("synthetic positive reply did not complete")
		}
		assert.Equal(t, before, f.snapshot(t))
		return
	}
	once.Do(func() { close(release) })
	var outcome delivery.Outcome
	select {
	case outcome = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("synthetic confirmed reply did not complete")
	}
	require.Equal(t, delivery.Deferred, outcome.Kind)
	require.Equal(t, "telegram_rate_limit", outcome.Reason)
	require.EqualValues(t, 120, outcome.RetryAfter)
	assert.NoError(t, f.finish(outcome), "a matching terminal negative must be retained as a wire fact")
	var confirmedAttempt int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT last_confirmed_attempt FROM core.`+f.table+` WHERE id=$1`, f.id).
			Scan(&confirmedAttempt),
	)
	assert.Equal(t, f.attempt, confirmedAttempt)
	if phase == "negative-replay" {
		checkRecoveredNegativeReplay(t, f, outcome)
		return
	}
	if phase == "cancelled-after-reply" {
		require.NoError(t, f.cancel())
	}
	before := f.snapshot(t)
	require.Error(
		t,
		f.finish(delivery.Outcome{Kind: delivery.Succeeded, MessageID: 91}),
		"confirmed rejection fences a contradictory positive completion",
	)
	assert.Equal(
		t,
		before,
		f.snapshot(t),
		"rejected success preserves owner, payload, pacing, queue and business sources",
	)
	var marker, state string
	var attempt, resends, messageID int64
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT jsonb_build_array(last_uncertain_attempt,last_uncertain_reason,last_uncertain_recorded_at)::text,state,`+f.attemptColumn+`,uncertain_resends,`+f.messageColumn+` FROM core.`+f.table+` WHERE id=$1`, f.id).
			Scan(&marker, &state, &attempt, &resends, &messageID),
	)
	assert.Equal(t, originalMarker, marker, "confirmed reply does not rewrite the prior uncertainty evidence")
	assert.Equal(t, f.attempt, attempt)
	assert.Zero(t, resends)
	assert.Zero(t, messageID)
	if phase == "pending" {
		assert.Equal(t, "pending", state)
	} else {
		assert.Equal(t, "cancelled", state)
	}
}

func checkRecoveredNegativeReplay(t *testing.T, f recoveredReplyFixture, outcome delivery.Outcome) {
	t.Helper()
	var state string
	var noLease bool
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT state,lease_until IS NULL FROM core.`+f.table+` WHERE id=$1`, f.id).
			Scan(&state, &noLease),
	)
	require.Equal(t, "pending", state)
	require.True(t, noLease)
	before := f.snapshot(t)
	ownerBefore, followerBefore := recoveredReplaySchedule(t, f)
	var started, elapsed time.Time
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&started))
	time.Sleep(150 * time.Millisecond)
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&elapsed))
	require.GreaterOrEqual(t, elapsed.Sub(started), 100*time.Millisecond,
		"the actual database clock must advance before testing deadline rebasing")
	// Replay the observed receipt defensively; the HTTP server returned it once.
	replayErr := f.finish(outcome)
	ownerAfter, followerAfter := recoveredReplaySchedule(t, f)
	assert.Equal(t, before, f.snapshot(t), "receipt replay preserves owner, queue, pacing, lanes, fairness and sources")
	assert.Equal(t, ownerBefore, ownerAfter, "every owner field, including message ID and deadline, remains unchanged")
	assert.Equal(t, followerBefore, followerAfter, "receipt replay cannot postpone the same-chat follower")
	var problem *core.ProblemError
	if assert.ErrorAs(t, replayErr, &problem, "an already confirmed recovered negative is refused as stale") {
		code := "admin_message_stale_attempt"
		if f.table == "pass_registration_announcements" {
			code = "pass_announcement_stale"
		}
		assert.Equal(t, http.StatusConflict, problem.Status)
		assert.Equal(t, code, problem.Code)
	}
}

func recoveredReplaySchedule(t *testing.T, f recoveredReplyFixture) (string, time.Time) {
	t.Helper()
	var owner string
	var follower time.Time
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT to_jsonb(d)::text FROM core.`+f.table+` d WHERE id=$1`, f.id).
		Scan(&owner))
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT GREATEST(q.not_before,b.not_before,c.not_before)
FROM core.delivery_queue q
LEFT JOIN core.delivery_pacing b ON b.bot_id=q.bot_id AND b.chat=''
LEFT JOIN core.delivery_pacing c ON c.bot_id=q.bot_id AND c.chat=q.chat
JOIN core.admin_message_deliveries d ON q.owner_kind='admin' AND q.owner_key=d.id::text
JOIN core.admin_messages m ON m.id=d.message_id WHERE m.key='negative-replay-follower'`).Scan(&follower))
	return owner, follower
}

func TestLegacyAnnouncementPrewireRefusalRecapturesCandidate(t *testing.T) {
	t.Parallel()
	for _, refusal := range []string{"pause", "pacing", "expired-preparation"} {
		t.Run(refusal, func(t *testing.T) {
			t.Parallel()
			checkLegacyAnnouncementCapture(t, refusal)
		})
	}
}

func checkLegacyAnnouncementCapture(t *testing.T, refusal string) {
	t.Helper()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET thread_channel='-100123'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "legacy-prewire", passbooking.Booking{}))
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET state='unknown',failure='telegram_outcome_unknown'; UPDATE core.delivery_queue SET state='unknown' WHERE owner_kind='announcement'`,
	)
	require.NoError(t, err)
	require.NoError(t, s.RecoverRegistrationAnnouncements(t.Context()))
	var captureMissing bool
	var historicalMarker int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT rendered_text IS NULL,last_uncertain_attempt FROM core.pass_registration_announcements`).
			Scan(&captureMissing, &historicalMarker),
	)
	require.True(t, captureMissing)
	require.Zero(t, historicalMarker)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	first, found, err := s.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	refuseLegacyAnnouncement(t, db, s, first, refusal)
	var resends int64
	var renderedAdmitted bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT uncertain_resends,rendered_admitted FROM core.pass_registration_announcements WHERE id=$1`, first.ID).
			Scan(&resends, &renderedAdmitted),
	)
	assert.Zero(t, resends, "no refused preparation is an admitted wire")
	assert.False(t, renderedAdmitted)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET name='New eligible rendering',locale='en',role='leader',available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET pause_reason='',not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	restarted := passbooking.Service{DB: db, Delivery: s.Delivery}
	item, found, err := restarted.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	expected, err := passbooking.RegistrationAnnouncementText(item)
	require.NoError(t, err)
	require.NotEqual(t, expected, first.Text)
	assert.Equal(t, expected, item.Text, "the next eligible legacy wire captures its current candidate")
	gate, err := restarted.BeginRegistrationAnnouncement(
		t.Context(),
		delivery.Attempt{ID: item.ID, Generation: item.Attempts},
	)
	require.NoError(t, err)
	require.True(t, gate.Ready)
	var admittedText string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT rendered_admitted,rendered_text FROM core.pass_registration_announcements WHERE id=$1`, item.ID).
			Scan(&renderedAdmitted, &admittedText),
	)
	assert.True(t, renderedAdmitted)
	assert.Equal(t, item.Text, admittedText, "capture and admission are durable before the HTTP request")
	wires := make(chan []byte, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Error(readErr)
			return
		}
		wires <- body
		connection, _, hijackErr := http.NewResponseController(w).Hijack()
		if hijackErr != nil {
			t.Error(hijackErr)
			return
		}
		_ = connection.Close()
	}))
	t.Cleanup(server.Close)
	client := telegram.Client{Base: server.URL, Token: "synthetic"}
	var reply telegram.Message
	sendErr := client.Call(
		t.Context(),
		"sendMessage",
		map[string]any{"chat_id": -100123, "text": item.Text, "parse_mode": "HTML"},
		&reply,
	)
	require.Error(t, sendErr)
	firstWire := <-wires
	var wirePayload struct {
		Text string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(firstWire, &wirePayload))
	assert.Equal(t, expected, wirePayload.Text)
	require.NoError(
		t,
		restarted.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{
				ID:      item.ID,
				Attempt: item.Attempts,
				Outcome: telegram.DeliveryOutcome(reply.ID, sendErr),
			},
		),
	)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_registration_announcements SET name='Later render input',available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'`,
	)
	require.NoError(t, err)
	again, found, err := restarted.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, item.Text, again.Text, "after the first admission uncertainty preserves its captured text")
	gate, err = restarted.BeginRegistrationAnnouncement(
		t.Context(),
		delivery.Attempt{ID: again.ID, Generation: again.Attempts},
	)
	require.NoError(t, err)
	require.True(t, gate.Ready)
	sendErr = client.Call(
		t.Context(),
		"sendMessage",
		map[string]any{"chat_id": -100123, "text": again.Text, "parse_mode": "HTML"},
		&reply,
	)
	require.Error(t, sendErr)
	assert.Equal(t, firstWire, <-wires)
	require.NoError(
		t,
		restarted.CompleteRegistrationAnnouncement(
			t.Context(),
			passbooking.AnnouncementCompletion{
				ID:      again.ID,
				Attempt: again.Attempts,
				Outcome: telegram.DeliveryOutcome(reply.ID, sendErr),
			},
		),
	)
	var captured string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT rendered_text,uncertain_resends FROM core.pass_registration_announcements WHERE id=$1`, item.ID).
			Scan(&captured, &resends),
	)
	assert.Equal(t, item.Text, captured)
	assert.EqualValues(t, 2, resends)
}

func refuseLegacyAnnouncement(
	t *testing.T,
	db *pgxpool.Pool,
	s passbooking.Service,
	first passbooking.RegistrationAnnouncement,
	refusal string,
) {
	t.Helper()
	if refusal == "expired-preparation" {
		_, err := db.Exec(
			t.Context(),
			`UPDATE core.pass_registration_announcements SET lease_until=clock_timestamp()-interval '1 second'`,
		)
		require.NoError(t, err)
		return
	}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.delivery_pacing(bot_id,chat) VALUES($1,'') ON CONFLICT DO NOTHING`,
		s.Delivery.BotID,
	)
	require.NoError(t, err)
	query := `UPDATE core.delivery_pacing SET not_before=clock_timestamp()+interval '120 seconds' WHERE bot_id=$1 AND chat=''`
	expectedReason := "delivery_cooldown"
	if refusal == "pause" {
		query = `UPDATE core.delivery_pacing SET pause_reason='synthetic_operator_pause' WHERE bot_id=$1 AND chat=''`
		expectedReason = "synthetic_operator_pause"
	}
	_, err = db.Exec(t.Context(), query, s.Delivery.BotID)
	require.NoError(t, err)
	gate, err := s.BeginRegistrationAnnouncement(
		t.Context(),
		delivery.Attempt{ID: first.ID, Generation: first.Attempts},
	)
	require.NoError(t, err)
	require.False(t, gate.Ready)
	assert.Equal(t, expectedReason, gate.Reason)
}
