package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

type notificationRuntimeFixture struct {
	f                *fixture
	table            string
	endpoint         string
	receiptTable     string
	receiptPredicate string
	first            int64
	second           int64
	other            int64
	deliver          func(context.Context) error
}

type notificationRuntimeStatus struct {
	State                   string     `json:"state"`
	Attempt                 int64      `json:"attempt"`
	MessageID               int64      `json:"message_id"`
	Reason                  string     `json:"reason"`
	FailureCount            int64      `json:"failure_count"`
	FollowupPending         bool       `json:"followup_pending"`
	FollowupFailure         string     `json:"followup_failure"`
	FollowupAttempts        int64      `json:"followup_attempts"`
	AvailableAt             time.Time  `json:"available_at"`
	LastUncertainAttempt    int64      `json:"last_uncertain_attempt"`
	LastUncertainReason     string     `json:"last_uncertain_reason"`
	LastUncertainRecordedAt *time.Time `json:"last_uncertain_recorded_at"`
	UncertainResends        int64      `json:"uncertain_resends"`
}

func notificationRuntime(t *testing.T, domain string) *notificationRuntimeFixture {
	t.Helper()
	switch domain {
	case "orders":
		return orderNotificationRuntime(t)
	case "registration":
		return passNotificationRuntime(t)
	case "massage":
		return massageNotificationRuntime(t)
	case "food":
		return foodNotificationRuntime(t)
	default:
		t.Fatalf("unknown notification domain %s", domain)
		return nil
	}
}

func orderNotificationRuntime(t *testing.T) *notificationRuntimeFixture {
	t.Helper()
	f := setup(t)
	first, _ := cashOrder(t, f, "runtime-first")
	second, _ := cashOrder(t, f, "runtime-second")
	other, _ := cashOrder(t, f, "runtime-other")
	_, err := f.b.API.ExecuteOrder(t.Context(), "bob", orderCommand("accept", other))
	require.NoError(t, err)
	r := &notificationRuntimeFixture{
		f:            f,
		table:        "core.order_notifications",
		endpoint:     "/internal/notifications/",
		receiptTable: "bot.notification_deliveries",
		deliver:      f.b.DeliverNotifications,
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.order_notifications WHERE order_id=$1 AND recipient='bob'`, first.ID).
			Scan(&r.first),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.order_notifications WHERE order_id=$1 AND recipient='bob'`, second.ID).
			Scan(&r.second),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.order_notifications WHERE order_id=$1 AND recipient='alice'`, other.ID).
			Scan(&r.other),
	)
	r.receiptPredicate = "NEW.id=" + strconv.FormatInt(r.first, 10)
	return r
}

func passNotificationRuntime(t *testing.T) *notificationRuntimeFixture {
	t.Helper()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('dance',now()+interval '30 days');
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('dance',0,20,100,now()-interval '1 day');
 INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','bob');
 INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader'),('bob','follower')`,
	)
	require.NoError(t, err)
	service := passbooking.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
	for _, owner := range []string{"bob", "alice"} {
		_, err = service.Execute(t.Context(), owner, bookingCommand("solo", "runtime-"+owner, passbooking.Booking{}))
		require.NoError(t, err)
	}
	r := &notificationRuntimeFixture{
		f:            f,
		table:        "core.pass_notifications",
		endpoint:     "/internal/pass-notifications/",
		receiptTable: "bot.pass_notification_deliveries",
		deliver:      f.b.DeliverPassNotifications,
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT min(id),max(id) FROM core.pass_notifications WHERE recipient='bob'`).
			Scan(&r.first, &r.second),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT min(id) FROM core.pass_notifications WHERE recipient='alice'`).
			Scan(&r.other),
	)
	require.NotEqual(t, r.first, r.second)
	r.receiptPredicate = "NEW.notice_id=" + strconv.FormatInt(r.first, 10)
	return r
}

func massageNotificationRuntime(t *testing.T) *notificationRuntimeFixture {
	t.Helper()
	f := massageBotFixture(t)
	service := massage.Service{DB: f.db, Delivery: syntheticDeliverySettings()}
	first, err := service.Execute(t.Context(), "alice", massageBook("runtime-first", "bob", 4, 1))
	require.NoError(t, err)
	second, err := service.Execute(t.Context(), "alice", massageBook("runtime-second", "bob", 6, 1))
	require.NoError(t, err)
	r := &notificationRuntimeFixture{
		f:            f,
		table:        "core.massage_notices",
		endpoint:     "/internal/massage-notifications/bob/",
		receiptTable: "bot.massage_deliveries",
		deliver:      f.b.DeliverMassageNotifications,
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.massage_notices WHERE booking_id=$1 AND owner='bob' AND kind='booked'`, first.ID).
			Scan(&r.first),
	)
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT id FROM core.massage_notices WHERE booking_id=$1 AND owner='bob' AND kind='booked'`, second.ID).
			Scan(&r.second),
	)
	// A due recipient fixture avoids changing the domain clock or the booking schedule.
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `INSERT INTO core.massage_notices(booking_id,owner,kind,bot_id) VALUES($1,'alice','prior_short',$2) RETURNING id`, first.ID, syntheticDeliverySettings().BotID).
			Scan(&r.other),
	)
	indexSyntheticNotificationRows(t, f.db, "massage")
	r.receiptPredicate = "NEW.notice_id=" + strconv.FormatInt(r.first, 10)
	return r
}

func foodNotificationRuntime(t *testing.T) *notificationRuntimeFixture {
	t.Helper()
	f, _ := foodBotFixture(t)
	r := &notificationRuntimeFixture{
		f:            f,
		table:        "core.food_notifications",
		endpoint:     "/internal/food/notifications/",
		receiptTable: "bot.order_cards",
		deliver:      f.b.DeliverFoodNotifications,
	}
	// These rows are explicit synthetic transport fixtures, not claimed business receipts.
	for index, target := range []struct {
		owner, subject string
		id             *int64
	}{{"bob", "first", &r.first}, {"bob", "second", &r.second}, {"alice", "other", &r.other}} {
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `INSERT INTO core.food_notifications(event_id,owner,kind,subject,payload,bot_id) VALUES('food-bot',$1,'paid',$2,'{}',$3) RETURNING id`, target.owner, target.subject, syntheticDeliverySettings().BotID).
				Scan(target.id),
			"fixture %d",
			index,
		)
	}
	indexSyntheticNotificationRows(t, f.db, "food")
	r.receiptPredicate = "NEW.card_key='food:notification:" + strconv.FormatInt(r.first, 10) + "'"
	return r
}

func foodReviewNotificationRuntime(t *testing.T) *notificationRuntimeFixture {
	t.Helper()
	f, service := foodBotFixture(t)
	order, err := service.Execute(t.Context(), "alice", legacyfood.Command{
		EventID: "food-bot", Name: "toggle_activity", Activity: "open", Key: "wire-party",
	})
	require.NoError(t, err)
	order, err = service.Execute(t.Context(), "alice", legacyfood.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "begin_payment", Kind: legacyfood.Activity, Key: "wire-pay",
	})
	require.NoError(t, err)
	proof, err := f.b.API.UploadProof(
		t.Context(),
		"alice",
		"receipt.txt",
		[]byte("synthetic notification review proof"),
	)
	require.NoError(t, err)
	order, err = service.Execute(t.Context(), "alice", legacyfood.Command{
		EventID: order.EventID, OrderID: order.ID, Version: order.Version,
		Name: "submit_proof", Kind: legacyfood.Activity, Key: "wire-proof",
		Generation: order.ActivityPayment.Generation, ProofID: proof.ID,
	})
	require.NoError(t, err)
	require.Equal(t, legacyfood.Submitted, order.ActivityPayment.Status)
	r := &notificationRuntimeFixture{
		f: f, table: "core.food_notifications", endpoint: "/internal/food/notifications/",
		receiptTable: "bot.order_cards", deliver: f.b.DeliverFoodNotifications,
	}
	require.NoError(t, f.db.QueryRow(t.Context(),
		`SELECT id FROM core.food_notifications WHERE owner='bob' AND kind=$1 AND payload->>'order_id'=$2`,
		legacyfood.Submitted, order.ID).Scan(&r.first))
	return r
}

func bindDefaultNotificationPacing(t *testing.T, r *notificationRuntimeFixture) {
	t.Helper()
	cfg, err := config.Load("health", []byte("env: sandbox\n"), nil)
	require.NoError(t, err)
	cfg.Auth.Zitadel.BotID = strconv.FormatInt(syntheticDeliverySettings().BotID, 10)
	cfg.Telegram.Token = config.Secret(cfg.Auth.Zitadel.BotID + ":synthetic")
	settings, err := cfg.DeliverySettings()
	require.NoError(t, err)
	require.Equal(t, 5*time.Second, settings.UncertaintyRetryBaseOrDefault())
	require.Equal(t, 30*time.Second, settings.Fallback)
	services := appservices.NewServices(r.f.db, appservices.Options{
		LegacyOrderBotID: 77, Delivery: settings,
		NativeRegistrationAuthorizer: fixtureNativeRegistrationAuthorizer(r.f.db),
	})
	server := httptest.NewServer(api.Handler(services, r.f.b.Host.Signer, slog.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	r.f.b.API.Base, r.f.b.Host.Base = server.URL, server.URL
	r.f.b.Delivery = settings
}

func (r *notificationRuntimeFixture) status(t *testing.T, id int64) notificationRuntimeStatus {
	t.Helper()
	endpoint := r.endpoint
	if r.table == "core.massage_notices" && id == r.other {
		endpoint = "/internal/massage-notifications/alice/"
	}
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		r.f.b.Host.Base+endpoint+strconv.FormatInt(id, 10),
		http.NoBody,
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+r.f.b.Host.Signer.DeliveryToken())
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode, string(body))
	assert.NotContains(t, string(body), "delivery_text")
	assert.NotContains(t, string(body), "payload")
	var value notificationRuntimeStatus
	require.NoError(t, json.Unmarshal(body, &value))
	return value
}

func (r *notificationRuntimeFixture) wake(t *testing.T) {
	t.Helper()
	// Advance only explicit test clock columns; attempt generations and outcomes stay untouched.
	_, err := r.f.db.Exec(
		t.Context(),
		"UPDATE "+r.table+" SET available_at=clock_timestamp()-interval '1 second'; UPDATE core.delivery_pacing SET not_before=clock_timestamp()-interval '1 second'; UPDATE core.delivery_queue SET not_before=clock_timestamp()-interval '1 second'",
	)
	require.NoError(t, err)
}

func (r *notificationRuntimeFixture) failFollowup(t *testing.T) func() {
	t.Helper()
	sql := `CREATE FUNCTION bot.synthetic_notification_receipt_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF ` + r.receiptPredicate + ` THEN RAISE EXCEPTION 'synthetic follow-up failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER synthetic_notification_receipt_failure BEFORE INSERT ON ` + r.receiptTable + ` FOR EACH ROW EXECUTE FUNCTION bot.synthetic_notification_receipt_failure()`
	_, err := r.f.db.Exec(t.Context(), sql)
	require.NoError(t, err)
	cleanup := func() {
		_, cleanupErr := r.f.db.Exec(
			context.WithoutCancel(t.Context()),
			"DROP TRIGGER IF EXISTS synthetic_notification_receipt_failure ON "+r.receiptTable+"; DROP FUNCTION IF EXISTS bot.synthetic_notification_receipt_failure()",
		)
		require.NoError(t, cleanupErr)
	}
	t.Cleanup(cleanup)
	return cleanup
}

type notificationWireSpy struct {
	mu        sync.Mutex
	firstText map[int64]string
	counts    map[int64]int
	mode      string
}

func attachNotificationWireSpy(t *testing.T, f *fixture, mode string) *notificationWireSpy {
	t.Helper()
	target, err := url.Parse(f.fake.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	spy := &notificationWireSpy{firstText: map[int64]string{}, counts: map[int64]int{}, mode: mode}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			body, readErr := io.ReadAll(r.Body)
			if readErr != nil {
				t.Error(readErr)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			var message struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			if decodeErr := json.Unmarshal(body, &message); decodeErr != nil {
				t.Error(decodeErr)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if spy.reject(w, message.ChatID, message.Text) {
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	f.b.TG.Base = server.URL
	return spy
}

func (s *notificationWireSpy) reject(w http.ResponseWriter, chat int64, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstText[chat] == "" {
		s.firstText[chat] = text
	}
	if s.firstText[chat] != text {
		return false
	}
	s.counts[chat]++
	if chat != 202 {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case s.mode == "missing_rate" && s.counts[chat] <= 2:
		_, _ = fmt.Fprint(w, `{"ok":false,"error_code":429,"description":"synthetic missing cooldown"}`)
		return true
	case s.mode == "rate" && s.counts[chat] <= 2:
		_, _ = fmt.Fprint(
			w,
			`{"ok":false,"error_code":429,"description":"synthetic rate limit","parameters":{"retry_after":60}}`,
		)
		return true
	case s.mode == "unknown" && s.counts[chat] == 1:
		_, _ = fmt.Fprint(w, `{"ok":true,"result":`)
		return true
	default:
		return false
	}
}

func (s *notificationWireSpy) calls(chat int64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[chat]
}

func TestNotificationDeliveryRepeated429ThenConsentRevocation(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			spy := attachNotificationWireSpy(t, r.f, "rate")
			for attempt := range 2 {
				require.NoError(t, r.deliver(t.Context()))
				status := r.status(t, r.first)
				assert.Equal(t, "pending", status.State)
				assert.Equal(t, "telegram_rate_limit", status.Reason)
				assert.Zero(t, status.FailureCount)
				assert.True(t, status.AvailableAt.After(time.Now()))
				assert.Equal(t, attempt+1, spy.calls(202))
				require.NoError(t, r.deliver(t.Context()))
				assert.Equal(t, attempt+1, spy.calls(202), "durable cooldown forbids immediate resend")
				r.wake(t)
			}
			_, err := r.f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='bob'`)
			require.NoError(t, err)
			require.NoError(t, r.deliver(t.Context()))
			assert.Equal(t, "cancelled", r.status(t, r.first).State)
			assert.Equal(t, 2, spy.calls(202), "fresh consent wins after the persisted delay")
		})
	}
}

func TestNotificationDeliveryUnknownBlocksOnlyItsLane(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			spy := attachNotificationWireSpy(t, r.f, "unknown")
			require.NoError(t, r.deliver(t.Context()))
			assert.Equal(t, "pending", r.status(t, r.first).State)
			require.NoError(t, r.deliver(t.Context()))
			assert.Equal(t, 1, spy.calls(202))
			assert.Equal(t, "pending", r.status(t, r.second).State)
			assert.Equal(t, "sent", r.status(t, r.other).State)
			assert.Positive(t, spy.calls(101), "an unrelated chat is not held behind uncertainty")
			r.wake(t)
			require.NoError(t, r.deliver(t.Context()))
			assert.Equal(t, "sent", r.status(t, r.first).State)
			assert.Equal(t, 2, spy.calls(202), "the same notification retries after its backoff")
		})
	}
}

func TestNotificationDeliveryKnownResultPrecedesFollowupFailure(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r, spy, before, restore := notificationSQLFollowupFailure(t, domain)
			r.expireNotificationLeases(t)
			r.wake(t)
			err := observeNotificationSQLStop(t, r)
			require.ErrorIs(t, err, core.ErrDatabase)
			assert.NotContains(t, err.Error(), "synthetic follow-up failure")
			assert.Equal(t, 1, spy.calls(202), "sent transport must not be repeated")
			assert.Equal(t, "pending", r.status(t, r.second).State)
			failed := r.status(t, r.first)
			assert.Equal(t, before.MessageID, failed.MessageID)
			assert.True(t, failed.FollowupPending)
			assert.Zero(t, failed.FollowupAttempts, "SQL failure must not schedule an ordinary retry")
			restore()
			r.restartNotificationOwner(t, domain)
			r.finishNotificationFollowup(t, spy, before)
		})
	}
}

// Observe the actual receipt SQL failure, not the beginning of a batch: other
// lanes may legitimately be selected before this failed follow-up.
type notificationSQLCutoff struct {
	failed        atomic.Bool
	queriesAfter  atomic.Int64
	requestsAfter atomic.Int64
}

func (c *notificationSQLCutoff) TraceQueryStart(
	ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData,
) context.Context {
	if c.failed.Load() {
		c.queriesAfter.Add(1)
	}
	return ctx
}

func (c *notificationSQLCutoff) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var failure *pgconn.PgError
	if errors.As(data.Err, &failure) && failure.Code == "P0001" && failure.Message == "synthetic follow-up failure" {
		c.failed.Store(true)
	}
}

func (c *notificationSQLCutoff) client(previous *http.Client) *http.Client {
	client := http.Client{}
	if previous != nil {
		client = *previous
	}
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = qaArchiveBoundaryTransport(func(request *http.Request) (*http.Response, error) {
		if c.failed.Load() {
			c.requestsAfter.Add(1)
		}
		return transport.RoundTrip(request)
	})
	return &client
}

func observeNotificationSQLStop(t *testing.T, r *notificationRuntimeFixture) error {
	t.Helper()
	cutoff := &notificationSQLCutoff{}
	config := r.f.db.Config()
	config.ConnConfig.Tracer = cutoff
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	defer pool.Close()
	b := r.f.b
	previousDB, previousAPI, previousHost, previousTG := b.DB, b.API.HTTP, b.Host.HTTP, b.TG.HTTP
	b.DB = pool
	b.API.HTTP, b.Host.HTTP, b.TG.HTTP = cutoff.client(
		previousAPI,
	), cutoff.client(
		previousHost,
	), cutoff.client(
		previousTG,
	)
	defer func() {
		b.DB, b.API.HTTP, b.Host.HTTP, b.TG.HTTP = previousDB, previousAPI, previousHost, previousTG
	}()
	err = r.deliver(t.Context())
	require.True(t, cutoff.failed.Load(), "the real receipt trigger must reach the observed SQL boundary")
	require.Zero(t, cutoff.queriesAfter.Load(), "no later local SQL after the fatal receipt failure")
	require.Zero(
		t,
		cutoff.requestsAfter.Load(),
		"no later API, Host or Telegram request after the fatal receipt failure",
	)
	return err
}

func notificationSQLFollowupFailure(t *testing.T, domain string) (
	*notificationRuntimeFixture, *notificationWireSpy, notificationRuntimeStatus, func(),
) {
	t.Helper()
	r := notificationRuntime(t, domain)
	spy := attachNotificationWireSpy(t, r.f, "")
	restore := r.failFollowup(t)
	err := r.deliver(t.Context())
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), "synthetic follow-up failure")
	before := r.status(t, r.first)
	require.Equal(t, "sent", before.State)
	require.Positive(t, before.MessageID)
	require.True(t, before.FollowupPending)
	require.Empty(t, before.FollowupFailure)
	require.Zero(t, before.FollowupAttempts)
	require.Equal(t, 1, spy.calls(202))
	var leaseLive bool
	require.NoError(t, r.f.db.QueryRow(t.Context(),
		"SELECT lease_until>clock_timestamp() FROM "+r.table+" WHERE id=$1", r.first).Scan(&leaseLive))
	require.True(t, leaseLive, "SQL stop preserves the live follow-up claim")
	return r, spy, before, restore
}

func (r *notificationRuntimeFixture) restartNotificationOwner(t *testing.T, domain string) {
	t.Helper()
	previous := r.f.b
	r.f.b = &bot.Bot{
		DB: previous.DB, API: previous.API, Host: previous.Host, TG: previous.TG,
		Delivery: previous.Delivery, OrderEventID: previous.OrderEventID, Model: previous.Model,
	}
	switch domain {
	case "orders":
		r.deliver = r.f.b.DeliverNotifications
	case "registration":
		r.deliver = r.f.b.DeliverPassNotifications
	case "massage":
		r.deliver = r.f.b.DeliverMassageNotifications
	case "food":
		r.deliver = r.f.b.DeliverFoodNotifications
	default:
		t.Fatal("unknown notification domain")
	}
}

func (r *notificationRuntimeFixture) finishNotificationFollowup(
	t *testing.T, spy *notificationWireSpy, before notificationRuntimeStatus,
) {
	t.Helper()
	r.expireNotificationLeases(t)
	r.wake(t)
	require.NoError(t, r.deliver(t.Context()))
	after := r.status(t, r.first)
	assert.Equal(t, before.MessageID, after.MessageID)
	assert.Equal(t, "sent", after.State)
	assert.False(t, after.FollowupPending)
	assert.Empty(t, after.FollowupFailure)
	assert.Equal(t, 1, spy.calls(202), "reconstructed owner resumes follow-up, not transport")
	beforeSuccessor := notificationSinkMessageIDs(t, r.f, 202)
	require.Contains(t, beforeSuccessor, before.MessageID)
	r.wake(t)
	require.NoError(t, r.deliver(t.Context()))
	successor := r.status(t, r.second)
	require.Equal(t, "sent", successor.State, "same-chat successor follows completed continuation")
	require.Positive(t, successor.MessageID)
	require.NotEqual(t, before.MessageID, successor.MessageID)
	assert.Equal(t, "sent", r.status(t, r.other).State, "other lane progresses once invocation is healthy")
	expectedIDs := beforeSuccessor
	expectedIDs = append(expectedIDs, successor.MessageID)
	require.ElementsMatch(t, expectedIDs, notificationSinkMessageIDs(t, r.f, 202),
		"only the legitimate successor adds a message, even when its text matches the first")
	callsAfterSuccessor := spy.calls(202)
	r.wake(t)
	require.NoError(t, r.deliver(t.Context()))
	assert.Equal(t, callsAfterSuccessor, spy.calls(202), "terminal receipt replay makes no repeat request")
	assert.Equal(t, before.MessageID, r.status(t, r.first).MessageID)
	assert.Equal(t, successor.MessageID, r.status(t, r.second).MessageID)
	assert.ElementsMatch(t, expectedIDs, notificationSinkMessageIDs(t, r.f, 202))
}

func notificationSinkMessageIDs(t *testing.T, f *fixture, chat int64) []int64 {
	t.Helper()
	var ids []int64
	for _, message := range chatMessages(t, f, chat) {
		ids = append(ids, message.ID)
	}
	return ids
}

// This provider is deliberately outside SQL: a lookup transport EOF must retain
// ordinary follow-up retry accounting rather than become a fatal database error.
type notificationFollowupProvider struct {
	token func(string) string
	calls int
}

func (p *notificationFollowupProvider) Telegram(_ context.Context, sender int64) (identity.User, error) {
	if sender == 202 {
		p.calls++
		return identity.User{}, io.ErrUnexpectedEOF
	}
	owner, ok := identity.Subject(sender)
	if !ok {
		return identity.User{}, identity.ErrZitadelIdentity
	}
	return identity.User{Owner: owner, Subject: owner}, nil
}

func (p *notificationFollowupProvider) Exchange(_ context.Context, subject string) (string, error) {
	return p.token(subject), nil
}

func TestNotificationFollowupProviderFailureKeepsRetryAccounting(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			// Establish a real sent-but-unfinished receipt, then remove the SQL
			// fault completely before exercising this separate provider boundary.
			r, spy, before, restore := notificationSQLFollowupFailure(t, domain)
			restore()
			original := r.f.b.API
			provider := &notificationFollowupProvider{token: original.SandboxToken}
			r.f.b.API.Links, r.f.b.API.Exchange = provider, provider
			r.expireNotificationLeases(t)
			r.wake(t)
			err := r.deliver(t.Context())
			require.False(t, core.IsDatabaseFailure(err))
			require.NoError(t, err, "ordinary identity failure is persisted as follow-up deferral")
			require.Positive(t, provider.calls)
			deferred := r.status(t, r.first)
			require.Equal(t, before.MessageID, deferred.MessageID)
			require.Equal(t, "sent", deferred.State)
			require.True(t, deferred.FollowupPending)
			require.Equal(t, "notification_followup_unavailable", deferred.FollowupFailure)
			require.EqualValues(t, 1, deferred.FollowupAttempts)
			require.True(t, deferred.AvailableAt.After(time.Now()))
			require.Equal(t, "pending", r.status(t, r.second).State)
			require.Equal(t, "sent", r.status(t, r.other).State)
			require.Equal(t, 1, spy.calls(202))
			r.f.b.API = original
			r.restartNotificationOwner(t, domain)
			r.finishNotificationFollowup(t, spy, before)
		})
	}
}

func (r *notificationRuntimeFixture) expireNotificationLeases(t *testing.T) {
	t.Helper()
	// The failed invocation has stopped. Model expiry of its claim clocks;
	// retain state, attempt generation, message ID, payload and continuation.
	_, err := r.f.db.Exec(t.Context(), "UPDATE "+r.table+
		" SET lease_until=clock_timestamp()-interval '1 second' WHERE lease_until IS NOT NULL AND delivery_state IN ('pending','sent')")
	require.NoError(t, err)
}
func (r *notificationRuntimeFixture) prepare(t *testing.T) {
	t.Helper()
	var err error
	switch r.table {
	case "core.order_notifications":
		_, err = r.f.b.Host.PendingNotifications(t.Context())
	case "core.pass_notifications":
		_, err = r.f.b.Host.PendingPassNotifications(t.Context())
	case "core.massage_notices":
		_, err = r.f.b.Host.MassageDeliveryNotices(t.Context(), "bob")
	case "core.food_notifications":
		_, err = r.f.b.Host.FoodNotifications(t.Context())
	default:
		t.Fatal("unknown prepared outbox")
	}
	require.NoError(t, err)
}

func (r *notificationRuntimeFixture) postAttempt(t *testing.T, action string, input any, want int) []byte {
	t.Helper()
	if attempt, ok := input.(delivery.Attempt); ok && action == "begin" {
		input = struct {
			delivery.Attempt
			Wire *notificationwire.Payload `json:"wire"`
		}{attempt, notificationTestWire()}
	}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		r.f.b.Host.Base+r.endpoint+action,
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+r.f.b.Host.Signer.DeliveryToken())
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	result, err := io.ReadAll(io.LimitReader(response.Body, 8192))
	require.NoError(t, err)
	require.Equal(t, want, response.StatusCode, string(result))
	return result
}

func TestNotificationDeliveryExactAttemptAndExpiredSend(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			spy := attachNotificationWireSpy(t, r.f, "")
			r.prepare(t)
			first := r.status(t, r.first)
			require.Positive(t, first.Attempt)
			_, err := r.f.db.Exec(
				t.Context(),
				"UPDATE "+r.table+" SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1",
				r.first,
			)
			require.NoError(t, err)
			r.prepare(t)
			current := r.status(t, r.first)
			require.Greater(t, current.Attempt, first.Attempt)
			r.postAttempt(t, "begin", delivery.Attempt{ID: r.first, Generation: first.Attempt}, http.StatusConflict)
			r.postAttempt(
				t,
				"complete",
				map[string]any{
					"id":      r.first,
					"attempt": first.Attempt,
					"outcome": delivery.Outcome{Kind: delivery.Cancelled, Reason: "synthetic_stale_attempt"},
				},
				http.StatusConflict,
			)
			var admission delivery.Admission
			require.NoError(
				t,
				json.Unmarshal(
					r.postAttempt(
						t,
						"begin",
						delivery.Attempt{ID: r.first, Generation: current.Attempt},
						http.StatusOK,
					),
					&admission,
				),
			)
			require.True(t, admission.Ready)
			_, err = r.f.db.Exec(
				t.Context(),
				"UPDATE "+r.table+" SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1",
				r.first,
			)
			require.NoError(t, err)
			r.prepare(t)
			expired := r.status(t, r.first)
			assert.Equal(t, "pending", expired.State)
			assert.Equal(t, current.Attempt, expired.Attempt, "an expired send is not a fresh attempt")
			r.postAttempt(t, "begin", delivery.Attempt{ID: r.first, Generation: current.Attempt}, http.StatusConflict)
			assert.Zero(t, spy.calls(202), "claim/lease recovery does not invent a transport send")
		})
	}
}

func TestNotificationDeliveryUnboundHeadIsNotAdopted(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			spy := attachNotificationWireSpy(t, r.f, "")
			tx, err := r.f.db.Begin(t.Context())
			require.NoError(t, err)
			_, err = tx.Exec(
				t.Context(),
				"UPDATE "+r.table+" SET bot_id=NULL,delivery_chat=0,delivery_state='paused',failure='notification_identity_unavailable' WHERE id=$1",
				r.first,
			)
			require.NoError(t, err)
			_, err = tx.Exec(
				t.Context(),
				"DELETE FROM core.delivery_queue WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key='send'",
				syntheticDeliverySettings().BotID,
				string(notificationQueueOwner(domain)),
				strconv.FormatInt(r.first, 10),
			)
			require.NoError(t, err)
			require.NoError(t, tx.Commit(t.Context()))
			require.NoError(t, err)
			require.NoError(t, r.deliver(t.Context()))
			r.wake(t)
			require.NoError(t, r.deliver(t.Context()))
			var unbound bool
			var attempt int64
			require.NoError(
				t,
				r.f.db.QueryRow(t.Context(), "SELECT bot_id IS NULL,delivery_attempt FROM "+r.table+" WHERE id=$1", r.first).
					Scan(&unbound, &attempt),
			)
			assert.True(t, unbound)
			assert.Zero(t, attempt)
			assert.Positive(t, spy.calls(202), "a configured new intent does not adopt an unbound predecessor")
			assert.Equal(
				t,
				"sent",
				r.status(t, r.second).State,
				"unbound intent does not invent a legacy queue barrier",
			)
			assert.Equal(t, "sent", r.status(t, r.other).State)
		})
	}
}

func notificationTestWire() *notificationwire.Payload {
	return &notificationwire.Payload{Text: "Synthetic notification", Markup: json.RawMessage(`{"inline_keyboard":[]}`)}
}
