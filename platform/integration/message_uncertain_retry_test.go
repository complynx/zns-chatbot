package integration_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

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
			assert.InDelta(t, 30*(1<<sent), delay, 2)
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
	case "later-429":
		mutation = "last_uncertain_attempt=last_uncertain_attempt-1,failure='telegram_uncertain_retry_exhausted'"
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
