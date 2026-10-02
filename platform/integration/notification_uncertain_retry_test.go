package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotificationUncertainRetrySourceExpiresDuringTransportWait(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"food", "massage"} {
		for _, lock := range []string{"lane", "pacing"} {
			for _, phase := range []string{"initial", "retry", "exhausted"} {
				t.Run(domain+"/"+lock+"/"+phase, func(t *testing.T) {
					t.Parallel()
					notificationSourceExpiresDuringTransportWait(t, domain, lock, phase)
				})
			}
		}
	}
}

func notificationSourceExpiresDuringTransportWait(t *testing.T, domain, lock, phase string) {
	t.Helper()
	r := notificationRuntime(t, domain)
	_, pacingErr := r.f.db.Exec(
		t.Context(),
		`INSERT INTO core.delivery_pacing(bot_id,chat,not_before) VALUES($1,'','-infinity'),($1,'202','-infinity') ON CONFLICT DO NOTHING`,
		syntheticDeliverySettings().BotID,
	)
	require.NoError(t, pacingErr)
	if phase != "initial" {
		loss := &notificationLostResponse{drops: 1}
		r.f.b.TG.HTTP = &http.Client{Transport: loss}
		require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
		requireNotificationAccepted(t, r, loss)
		r.wake(t)
	}
	if phase == "exhausted" {
		_, err := r.f.db.Exec(
			t.Context(),
			"UPDATE "+r.table+" SET uncertain_resends=3 WHERE id=$1",
			r.first,
		)
		require.NoError(t, err)
	}
	var deadline time.Time
	if domain == "food" {
		_, err := r.f.db.Exec(
			t.Context(),
			`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price)
 VALUES('food-bot','bob',1,'assigned','follower','solo','bob',clock_timestamp(),clock_timestamp(),100);
 UPDATE core.food_notifications SET kind='no_order_last' WHERE subject='first'`,
		)
		require.NoError(t, err)
		require.NoError(
			t,
			r.f.db.QueryRow(t.Context(), `UPDATE core.food_events SET deadline=clock_timestamp()+interval '1 second' WHERE event_id='food-bot' RETURNING deadline`).
				Scan(&deadline),
		)
	} else {
		_, err := r.f.db.Exec(
			t.Context(),
			"UPDATE core.massage_notices SET kind='additional' WHERE id=$1",
			r.first,
		)
		require.NoError(t, err)
		require.NoError(
			t,
			r.f.db.QueryRow(t.Context(), `UPDATE core.massage_bookings SET starts_at=clock_timestamp()+interval '1 second' WHERE id=(SELECT booking_id FROM core.massage_notices WHERE id=$1) RETURNING starts_at`, r.first).
				Scan(&deadline),
		)
	}
	r.prepare(t)
	before := r.status(t, r.first)
	pacing := notificationPacingAndFairness(t, r)
	blocker, err := r.f.db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(context.Background()) }()
	var pid int
	require.NoError(t, blocker.QueryRow(t.Context(), "SELECT pg_backend_pid()").Scan(&pid))
	if lock == "lane" {
		_, err = blocker.Exec(
			t.Context(),
			"SELECT 1 FROM core.delivery_lanes WHERE bot_id=$1 AND chat='202' FOR UPDATE",
			syntheticDeliverySettings().BotID,
		)
	} else {
		_, err = blocker.Exec(
			t.Context(),
			"SELECT 1 FROM core.delivery_pacing WHERE bot_id=$1 AND chat='' FOR UPDATE",
			syntheticDeliverySettings().BotID,
		)
	}
	require.NoError(t, err)
	body, err := json.Marshal(struct {
		delivery.Attempt

		Wire *notificationwire.Payload `json:"wire"`
	}{delivery.Attempt{ID: r.first, Generation: before.Attempt}, notificationTestWire()})
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		r.f.b.Host.Base+r.endpoint+"begin",
		bytes.NewReader(body),
	)
	require.NoError(t, err)
	request.Header.Set("Authorization", "Bearer "+r.f.b.Host.Signer.DeliveryToken())
	responses := make(chan *http.Response, 1)
	failures := make(chan error, 1)
	go func() {
		response, callErr := http.DefaultClient.Do(request)
		if callErr != nil {
			failures <- callErr
			return
		}
		responses <- response
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := r.f.db.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))", pid).
			Scan(&waiting)
		return queryErr == nil && waiting
	}, 2*time.Second, 10*time.Millisecond, "admission actually waits behind the transport lock")
	waitNotificationEligibility(t, r, deadline.Add(time.Millisecond))
	require.NoError(t, blocker.Commit(t.Context()))
	select {
	case response := <-responses:
		defer response.Body.Close()
		require.Equal(t, http.StatusOK, response.StatusCode)
		var admission delivery.Admission
		require.NoError(t, json.NewDecoder(response.Body).Decode(&admission))
		assert.False(t, admission.Ready, "expired source cannot authorize a wire")
	case callErr := <-failures:
		require.NoError(t, callErr)
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	after := r.status(t, r.first)
	assert.Equal(t, "cancelled", after.State)
	assert.Equal(t, "notification_no_longer_current", after.Reason)
	assert.Equal(t, before.UncertainResends, after.UncertainResends)
	assert.Equal(t, before.Attempt, after.Attempt)
	assert.JSONEq(
		t,
		pacing,
		notificationPacingAndFairness(t, r),
		"cancelled source discards every transport reservation",
	)
}

// The sink receives the real message before the adapter loses its response.
type notificationLostResponse struct {
	mu            sync.Mutex
	text          string
	calls         int
	drops         int
	wires         []telegram.Send
	observe       func(*http.Request, telegram.Send) error
	observerError error
}

func (l *notificationLostResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	if !strings.HasSuffix(request.URL.Path, "/sendMessage") {
		return http.DefaultTransport.RoundTrip(request)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	_ = request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(body))
	var message telegram.Send
	if err = json.Unmarshal(body, &message); err != nil {
		return nil, err
	}
	if l.observe != nil {
		if err = l.observe(request, message); err != nil {
			l.mu.Lock()
			l.observerError = err
			l.mu.Unlock()
			return nil, err
		}
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil || message.ChatID != 202 {
		return response, err
	}
	l.mu.Lock()
	l.wires = append(l.wires, message)
	if l.text == "" {
		l.text = message.Text
	}
	primary := l.text == message.Text
	if primary {
		l.calls++
	}
	lost := primary && l.calls <= l.drops
	l.mu.Unlock()
	if !lost {
		return response, nil
	}
	_, err = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if err != nil {
		return nil, err
	}
	return nil, errors.New("synthetic accepted notification response lost")
}

func (l *notificationLostResponse) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

func (l *notificationLostResponse) payloads() []telegram.Send {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.wires)
}

func setNotificationLanguage(t *testing.T, r *notificationRuntimeFixture, language string) {
	t.Helper()
	_, err := r.f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='bob'`, language)
	require.NoError(t, err)
	pref, err := r.f.b.API.Preferences(t.Context(), "bob")
	require.NoError(t, err)
	require.Equal(t, language, pref.Language)
}

func notificationWireSnapshot(t *testing.T, r *notificationRuntimeFixture) *notificationwire.Payload {
	t.Helper()
	var raw []byte
	err := r.f.db.QueryRow(t.Context(), "SELECT delivery_wire_payload FROM "+r.table+" WHERE id=$1", r.first).Scan(&raw)
	require.NoError(t, err)
	wire, present, err := notificationwire.Decode(raw)
	require.NoError(t, err)
	if !present {
		return nil
	}
	return &wire
}

func TestNotificationUncertainRetryPreservesWireAfterLanguageChange(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			setNotificationLanguage(t, r, "en")
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			require.Equal(t, "pending", r.status(t, r.first).State)
			before := loss.payloads()
			require.Len(t, before, 1)
			require.NotEmpty(t, before[0].Text)
			if domain == "massage" {
				require.NotEmpty(t, before[0].Markup.Rows)
			}
			setNotificationLanguage(t, r, "ru")
			r.restartNotificationOwner(t, domain)
			r.wake(t)
			dispatch = exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			after := loss.payloads()
			require.GreaterOrEqual(t, len(after), 2)
			assert.Equal(t, before[0].Text, after[1].Text, "retry must preserve admitted wire text")
			assert.Equal(t, before[0].Markup, after[1].Markup, "retry must preserve actual buttons")
			assert.Equal(t, "sent", r.status(t, r.first).State)
		})
	}
}

func TestNotificationUncertainRetryCapturesLegacyNextWire(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			setNotificationLanguage(t, r, "en")
			original := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: original}
			require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
			requireNotificationAccepted(t, r, original)
			unknown := r.status(t, r.first)
			require.Equal(t, "pending", unknown.State)
			// A pre-upgrade unknown has a real lost response but no recorded wire.
			seedLegacyUnknownWithoutWire(t, r, domain)
			require.Nil(t, notificationWireSnapshot(t, r))
			assert.Equal(t, unknown.LastUncertainAttempt, r.status(t, r.first).LastUncertainAttempt)
			setNotificationLanguage(t, r, "ru")
			next := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: next}
			r.restartNotificationOwner(t, domain)
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			r.wake(t)
			require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
			requireNotificationAccepted(t, r, next)
			wire := notificationWireSnapshot(t, r)
			require.NotNil(t, wire)
			assert.NotEqual(
				t,
				original.payloads()[0].Text,
				wire.Text,
				"legacy capture describes next wire, not lost historical content",
			)
			assert.Equal(t, int64(1), r.status(t, r.first).UncertainResends)
			setNotificationLanguage(t, r, "en")
			r.restartNotificationOwner(t, domain)
			r.wake(t)
			require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
			actual := next.payloads()
			require.GreaterOrEqual(t, len(actual), 2)
			assert.Equal(t, actual[0].Text, actual[1].Text)
			assert.Equal(t, actual[0].Markup, actual[1].Markup)
			assert.Equal(t, int64(2), r.status(t, r.first).UncertainResends)
			assert.Equal(t, "sent", r.status(t, r.first).State)
		})
	}
}

func seedLegacyUnknownWithoutWire(t *testing.T, r *notificationRuntimeFixture, domain string) {
	t.Helper()
	tx, err := r.f.db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = tx.Exec(
		t.Context(),
		"UPDATE "+r.table+" SET delivery_wire_payload=NULL,delivery_state='unknown',lease_until=NULL WHERE id=$1",
		r.first,
	)
	require.NoError(t, err)
	_, err = tx.Exec(
		t.Context(),
		`UPDATE core.delivery_queue SET state='unknown' WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key='send'`,
		syntheticDeliverySettings().BotID,
		string(notificationQueueOwner(domain)),
		strconv.FormatInt(r.first, 10),
	)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
}

func TestNotificationUncertainRetryPreservesFoodReviewButtons(t *testing.T) {
	t.Parallel()
	r := foodReviewNotificationRuntime(t)
	setNotificationLanguage(t, r, "en")
	business := notificationBusinessSnapshot(t, r)
	loss := &notificationLostResponse{drops: 1}
	r.f.b.TG.HTTP = &http.Client{Transport: loss}
	require.NoError(t, r.f.b.DeliverFoodNotification(t.Context(), r.first))
	requireNotificationAccepted(t, r, loss)
	before := loss.payloads()
	require.Len(t, before, 1)
	require.NotEmpty(t, before[0].Markup.Rows)
	setNotificationLanguage(t, r, "ru")
	r.restartNotificationOwner(t, "food")
	r.wake(t)
	require.NoError(t, r.f.b.DeliverFoodNotification(t.Context(), r.first))
	after := loss.payloads()
	require.GreaterOrEqual(t, len(after), 2)
	assert.Equal(t, before[0].Text, after[1].Text)
	assert.Equal(t, before[0].Markup, after[1].Markup, "captions and exact callback tokens remain the admitted buttons")
	assert.Equal(t, "sent", r.status(t, r.first).State)
	assert.JSONEq(t, business, notificationBusinessSnapshot(t, r))
}

func notificationWireObserver(r *notificationRuntimeFixture) func(*http.Request, telegram.Send) error {
	return func(request *http.Request, send telegram.Send) error {
		if send.ChatID != 202 {
			return nil
		}
		var state string
		var raw []byte
		err := r.f.db.QueryRow(request.Context(), "SELECT delivery_state,delivery_wire_payload FROM "+r.table+" WHERE id=$1", r.first).
			Scan(&state, &raw)
		if err != nil {
			return err
		}
		wire, present, err := notificationwire.Decode(raw)
		if err != nil {
			return err
		}
		if state != "sending" || !present || wire.Text != send.Text {
			return errors.New("notification wire was not committed before transport")
		}
		var markup telegram.Markup
		if json.Unmarshal(wire.Markup, &markup) != nil || !reflect.DeepEqual(markup, send.Markup) {
			return fmt.Errorf(
				"notification markup was not committed before transport: stored=%#v actual=%#v",
				markup,
				send.Markup,
			)
		}
		return nil
	}
}

func TestNotificationUncertainRetryWireCommittedBeforeTransport(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 1, observe: notificationWireObserver(r)}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
			loss.mu.Lock()
			observerError := loss.observerError
			loss.mu.Unlock()
			require.NoError(t, observerError)
			requireNotificationAccepted(t, r, loss)
			require.NotNil(t, notificationWireSnapshot(t, r))
			assert.Equal(t, "pending", r.status(t, r.first).State)
		})
	}
}

func TestNotificationUncertainRetryPrewireDoesNotCapture(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			_, err := r.f.db.Exec(
				t.Context(),
				`INSERT INTO core.delivery_pacing(bot_id,chat,not_before) VALUES($1,'',clock_timestamp()+interval '1 minute') ON CONFLICT(bot_id,chat) DO UPDATE SET not_before=EXCLUDED.not_before`,
				syntheticDeliverySettings().BotID,
			)
			require.NoError(t, err)
			loss := &notificationLostResponse{}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
			assert.Empty(t, loss.payloads())
			assert.Nil(t, notificationWireSnapshot(t, r))
			current := r.status(t, r.first)
			assert.Equal(t, "pending", current.State)
			assert.Zero(t, current.UncertainResends)
		})
	}
}

func TestNotificationUncertainRetryMissing429KeepsConfiguredCooldown(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			bindDefaultNotificationPacing(t, r)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			original := r.status(t, r.first)
			spy := attachNotificationWireSpy(t, r.f, "missing_rate")
			r.f.b.TG.HTTP = nil
			r.wake(t)
			before := time.Now()
			require.NoError(t, dispatch(t.Context(), r.first))
			current := r.status(t, r.first)
			require.Equal(t, "pending", current.State)
			assert.Equal(t, int64(1), current.UncertainResends)
			assert.Equal(t, original.LastUncertainAttempt, current.LastUncertainAttempt)
			assert.Equal(t, original.LastUncertainRecordedAt, current.LastUncertainRecordedAt)
			assert.Zero(t, current.FailureCount)
			assert.True(t, current.AvailableAt.After(before.Add(29*time.Second)))
			assertNotificationCooldown(t, r, domain, before.Add(29*time.Second))
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(
				t,
				1,
				spy.calls(202),
				"missing provider cooldown cannot fall back to only the uncertainty base",
			)
			assert.Equal(t, int64(1), r.status(t, r.first).UncertainResends)
		})
	}
}

func assertNotificationCooldown(t *testing.T, r *notificationRuntimeFixture, domain string, earliest time.Time) {
	t.Helper()
	var global, chat, queue time.Time
	require.NoError(t, r.f.db.QueryRow(
		t.Context(),
		`SELECT max(not_before) FILTER(WHERE chat=''),max(not_before) FILTER(WHERE chat='202') FROM core.delivery_pacing WHERE bot_id=$1`,
		syntheticDeliverySettings().BotID,
	).Scan(&global, &chat))
	require.NoError(t, r.f.db.QueryRow(
		t.Context(),
		`SELECT not_before FROM core.delivery_queue WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key='send'`,
		syntheticDeliverySettings().BotID,
		string(notificationQueueOwner(domain)),
		strconv.FormatInt(r.first, 10),
	).Scan(&queue))
	assert.True(t, global.After(earliest))
	assert.True(t, chat.After(earliest))
	assert.True(t, queue.After(earliest))
}

func requireNotificationAccepted(t *testing.T, r *notificationRuntimeFixture, loss *notificationLostResponse) {
	t.Helper()
	_ = acceptedNotificationMessage(t, r, loss)
}

func acceptedNotificationMessage(
	t *testing.T,
	r *notificationRuntimeFixture,
	loss *notificationLostResponse,
) telegram.Message {
	t.Helper()
	loss.mu.Lock()
	text := loss.text
	loss.mu.Unlock()
	messages := chatMessages(t, r.f, 202)
	for _, message := range slices.Backward(messages) {
		if message.Text == text {
			return message
		}
	}
	require.FailNow(t, "the real sink must accept the exact notification before response loss")
	return telegram.Message{}
}

func TestNotificationUncertainRetrySurvivesRestart(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			business := notificationBusinessSnapshot(t, r)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			first := r.status(t, r.first)
			require.Equal(t, "pending", first.State)
			require.Equal(t, 1, loss.count())
			assert.GreaterOrEqual(
				t,
				time.Until(first.AvailableAt),
				r.f.b.Delivery.UncertaintyRetryBaseOrDefault()-time.Second,
			)
			assert.Equal(t, first.Attempt, first.LastUncertainAttempt)
			assert.Equal(t, "telegram_outcome_unknown", first.LastUncertainReason)
			require.NotNil(t, first.LastUncertainRecordedAt)
			assert.Zero(t, first.UncertainResends)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, 1, loss.count(), "no wire before durable backoff")
			r.restartNotificationOwner(t, domain)
			dispatch = exactNotificationDelivery(r, domain)
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			assert.Equal(t, first.AvailableAt, r.status(t, r.first).AvailableAt)
			r.wake(t)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, "sent", r.status(t, r.first).State)
			assert.Equal(t, int64(1), r.status(t, r.first).UncertainResends)
			assert.Equal(t, first.LastUncertainRecordedAt, r.status(t, r.first).LastUncertainRecordedAt)
			assert.Equal(t, 2, loss.count())
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, 2, loss.count(), "known success does not resend")
			assert.JSONEq(
				t,
				business,
				notificationBusinessSnapshot(t, r),
				"message retry must not replay booking or payment business",
			)
		})
	}
}

func TestNotificationUncertainRetryExhaustionReleasesFollower(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 4}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			for attempt := range 4 {
				require.NoError(t, dispatch(t.Context(), r.first))
				requireNotificationAccepted(t, r, loss)
				require.Equal(t, attempt+1, loss.count())
				state := r.status(t, r.first)
				assert.Equal(t, int64(attempt), state.UncertainResends)
				assert.Equal(t, state.Attempt, state.LastUncertainAttempt)
				assert.Equal(t, "telegram_outcome_unknown", state.LastUncertainReason)
				require.NotNil(t, state.LastUncertainRecordedAt)
				if attempt == 3 {
					assert.Equal(t, "failed", state.State)
					assert.Equal(t, "telegram_uncertain_retry_exhausted", state.Reason)
					break
				}
				require.Equal(t, "pending", state.State)
				assert.GreaterOrEqual(t, time.Until(state.AvailableAt),
					r.f.b.Delivery.UncertaintyRetryBaseOrDefault()*time.Duration(1<<attempt)-time.Second)
				require.NoError(t, dispatch(t.Context(), r.second))
				assert.Equal(t, "pending", r.status(t, r.second).State)
				r.restartNotificationOwner(t, domain)
				dispatch = exactNotificationDelivery(r, domain)
				require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
				r.wake(t)
			}
			r.wake(t)
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, 4, loss.count(), "exhaustion never resurrects")
			require.NoError(t, dispatch(t.Context(), r.second))
			assert.Equal(t, "sent", r.status(t, r.second).State)
		})
	}
}

func TestNotificationUncertainRetryLateAdmittedSuccess(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
			message := acceptedNotificationMessage(t, r, loss)
			before := r.status(t, r.first)
			require.Contains(t, []string{"unknown", "pending"}, before.State)
			completion := map[string]any{
				"id": r.first, "attempt": before.Attempt, "text": message.Text,
				"outcome": delivery.Outcome{Kind: delivery.Succeeded, MessageID: message.ID},
			}
			r.postAttempt(t, "complete", completion, http.StatusOK)
			assert.Equal(t, "sent", r.status(t, r.first).State)
			assert.Equal(t, message.ID, r.status(t, r.first).MessageID)
			r.postAttempt(t, "complete", completion, http.StatusOK)
			assert.Equal(t, 1, loss.count(), "late receipt resolution does not admit another send")
		})
	}
}

func TestNotificationUncertainRetryLateReceiptFencedByPrepare(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			require.NoError(t, exactNotificationDelivery(r, domain)(t.Context(), r.first))
			message := acceptedNotificationMessage(t, r, loss)
			before := r.status(t, r.first)
			r.wake(t)
			r.prepare(t)
			current := r.status(t, r.first)
			require.Greater(t, current.Attempt, before.Attempt)
			completion := map[string]any{
				"id": r.first, "attempt": before.Attempt, "text": message.Text,
				"outcome": delivery.Outcome{Kind: delivery.Succeeded, MessageID: message.ID},
			}
			r.postAttempt(t, "complete", completion, http.StatusConflict)
			completion["attempt"] = current.Attempt
			r.postAttempt(t, "complete", completion, http.StatusConflict)
			assert.Equal(t, "pending", r.status(t, r.first).State)
			assert.Zero(t, r.status(t, r.first).MessageID)
			assert.Zero(t, r.status(t, r.first).UncertainResends, "Prepare never admits a resend")
			assert.Equal(t, 1, loss.count())
		})
	}
}

func TestNotificationUncertainRetryTerminalReceiptOnly(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		for _, policy := range []string{"cancelled", "exhausted"} {
			t.Run(domain+"/"+policy, func(t *testing.T) {
				t.Parallel()
				r := notificationRuntime(t, domain)
				loss := &notificationLostResponse{drops: 10}
				r.f.b.TG.HTTP = &http.Client{Transport: loss}
				dispatch := exactNotificationDelivery(r, domain)
				require.NoError(t, dispatch(t.Context(), r.first))
				message := acceptedNotificationMessage(t, r, loss)
				if policy == "exhausted" {
					for range 3 {
						r.wake(t)
						require.NoError(t, dispatch(t.Context(), r.first))
					}
					require.Equal(t, "failed", r.status(t, r.first).State)
					message = acceptedNotificationMessage(t, r, loss)
				} else {
					// Recreate the persisted admitted-wire crash boundary before recovery.
					_, err := r.f.db.Exec(
						t.Context(),
						"UPDATE "+r.table+" SET delivery_state='sending',lease_until=clock_timestamp()-interval '1 second' WHERE id=$1",
						r.first,
					)
					require.NoError(t, err)
					_, err = r.f.db.Exec(t.Context(), "UPDATE core.users SET can_book=false WHERE id='bob'")
					require.NoError(t, err)
					require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
					require.Equal(t, "cancelled", r.status(t, r.first).State)
				}
				before := notificationReceiptSnapshot(t, r)
				state := r.status(t, r.first)
				completion := map[string]any{
					"id": r.first, "attempt": state.Attempt, "text": "ignored late receipt text",
					"outcome": delivery.Outcome{Kind: delivery.Succeeded, MessageID: message.ID},
				}
				r.postAttempt(t, "complete", completion, http.StatusOK)
				r.postAttempt(t, "complete", completion, http.StatusOK)
				after := notificationReceiptSnapshot(t, r)
				require.Equal(t, json.Number(strconv.FormatInt(message.ID, 10)), after["telegram_message_id"])
				after["telegram_message_id"] = before["telegram_message_id"]
				assert.Equal(t, before, after, "terminal receipt must change only the known message ID")
				positive := notificationReceiptSnapshot(t, r)
				r.postAttempt(t, "complete", map[string]any{
					"id": r.first, "attempt": state.Attempt,
					"outcome": delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 60},
				}, http.StatusConflict)
				assert.Equal(
					t,
					positive,
					notificationReceiptSnapshot(t, r),
					"confirmed negative cannot contradict a stored positive receipt",
				)
				completion["outcome"] = delivery.Outcome{Kind: delivery.Succeeded, MessageID: message.ID + 100000}
				r.postAttempt(t, "complete", completion, http.StatusConflict)
				assert.Equal(t, state.State, r.status(t, r.first).State)
				assert.False(t, r.status(t, r.first).FollowupPending)
			})
		}
	}
}

func notificationReceiptSnapshot(t *testing.T, r *notificationRuntimeFixture) map[string]any {
	t.Helper()
	owner := map[string]string{"core.order_notifications": "orders", "core.pass_notifications": "passes", "core.massage_notices": "massage", "core.food_notifications": "food"}[r.table]
	var raw []byte
	require.NoError(
		t,
		r.f.db.QueryRow(t.Context(), "SELECT to_jsonb(n) || jsonb_build_object('queue',(SELECT to_jsonb(q) FROM core.delivery_queue q WHERE q.bot_id=n.bot_id AND q.owner_key=n.id::text AND q.owner_kind=$2 AND q.effect_key='send')) FROM "+r.table+" n WHERE n.id=$1", r.first, owner).
			Scan(&raw),
	)
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&result))
	return result
}

// Capture complete delivery rows, including the follower and both pacing lanes.
func notificationDeliverySnapshot(t *testing.T, r *notificationRuntimeFixture) string {
	t.Helper()
	var raw []byte
	require.NoError(t, r.f.db.QueryRow(t.Context(),
		"SELECT jsonb_build_object('owners',(SELECT jsonb_agg(to_jsonb(n) ORDER BY n.id) FROM "+r.table+" n),"+
			"'queue',(SELECT jsonb_agg(to_jsonb(q) ORDER BY q.bot_id,q.owner_kind,q.owner_key,q.effect_key) FROM core.delivery_queue q),"+
			"'pacing',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.bot_id,p.chat) FROM core.delivery_pacing p))").Scan(&raw))
	return string(raw)
}
func TestNotificationUncertainRetryCountsRateLimitedSends(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			message := acceptedNotificationMessage(t, r, loss)
			original := r.status(t, r.first)
			spy := attachNotificationWireSpy(t, r.f, "rate")
			for attempt := range 2 {
				r.wake(t)
				require.NoError(t, dispatch(t.Context(), r.first))
				state := r.status(t, r.first)
				require.Equal(t, "pending", state.State)
				assert.Equal(t, int64(attempt+1), state.UncertainResends)
				assert.Equal(t, original.LastUncertainAttempt, state.LastUncertainAttempt)
				assert.Equal(t, original.LastUncertainRecordedAt, state.LastUncertainRecordedAt)
				assert.Zero(t, state.FailureCount)
				assert.GreaterOrEqual(t, time.Until(state.AvailableAt), 59*time.Second)
				r.postAttempt(t, "complete", map[string]any{
					"id": r.first, "attempt": state.Attempt, "text": message.Text,
					"outcome": delivery.Outcome{Kind: delivery.Succeeded, MessageID: message.ID},
				}, http.StatusConflict)
				require.NoError(t, dispatch(t.Context(), r.first))
				assert.Equal(t, attempt+1, spy.calls(202))
			}
			r.wake(t)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, "sent", r.status(t, r.first).State)
			assert.Equal(t, int64(3), r.status(t, r.first).UncertainResends)
			assert.Equal(t, 3, spy.calls(202))
		})
	}
}

func TestNotificationUncertainRetryThird429RetainsCooldown(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			business := notificationBusinessSnapshot(t, r)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			original := r.status(t, r.first)
			wire := notificationWireSnapshot(t, r)
			require.NotNil(t, wire)
			provider := notificationCooldownProvider(t, r, wire.Text)
			r.f.b.TG.Base = provider.URL
			for attempt := range 3 {
				waitNotificationEligibility(t, r, r.status(t, r.first).AvailableAt)
				var before time.Time
				require.NoError(t, r.f.db.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&before))
				require.NoError(t, dispatch(t.Context(), r.first))
				state := r.status(t, r.first)
				assert.Equal(t, "pending", state.State, "admitted 429 retains the provider cooldown")
				assert.Equal(t, "telegram_rate_limit", state.Reason)
				assert.Equal(t, int64(attempt+1), state.UncertainResends)
				assert.Equal(t, original.LastUncertainAttempt, state.LastUncertainAttempt)
				assert.Equal(t, original.LastUncertainReason, state.LastUncertainReason)
				assert.Equal(t, original.LastUncertainRecordedAt, state.LastUncertainRecordedAt)
				assert.Zero(t, state.FailureCount)
				assert.True(t, state.AvailableAt.After(before.Add(21*time.Second)))
				assertNotificationCooldown(t, r, domain, before.Add(21*time.Second))
				assertNotification429Projection(t, r, domain, state)
				assert.Equal(t, attempt+2, loss.count(), "first unknown plus actual admitted 429s")
				assert.Equal(t, wire, notificationWireSnapshot(t, r))
				payloads := loss.payloads()
				assert.Equal(
					t,
					payloads[0],
					payloads[len(payloads)-1],
					"actual retry wire remains the admitted payload",
				)
				require.NoError(t, dispatch(t.Context(), r.first))
				require.NoError(t, dispatch(t.Context(), r.second))
				assert.Equal(t, state, r.status(t, r.first), "no attempt or policy mutation before eligibility")
				assert.Equal(t, "pending", r.status(t, r.second).State)
			}
			deferred := r.status(t, r.first)
			var cooldown time.Time
			require.NoError(t, r.f.db.QueryRow(t.Context(),
				"SELECT max(not_before) FROM core.delivery_pacing WHERE bot_id=$1", syntheticDeliverySettings().BotID,
			).Scan(&cooldown))
			if deferred.AvailableAt.After(cooldown) {
				cooldown = deferred.AvailableAt
			}
			waitNotificationEligibility(t, r, cooldown)
			r.prepare(t)
			prepared := r.status(t, r.first)
			require.Equal(
				t,
				deferred.Attempt,
				prepared.Attempt,
				"policy-only preparation retains the confirmed generation",
			)
			var leaseLive bool
			require.NoError(t, r.f.db.QueryRow(t.Context(),
				"SELECT lease_until>clock_timestamp() FROM "+r.table+" WHERE id=$1", r.first).Scan(&leaseLive))
			require.True(t, leaseLive)
			beforeReplay := notificationDeliverySnapshot(t, r)
			r.postAttempt(t, "complete", map[string]any{
				"id": r.first, "attempt": deferred.Attempt,
				"outcome": delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 21},
			}, http.StatusOK)
			afterReplay := notificationDeliverySnapshot(t, r)
			assert.Equal(
				t,
				beforeReplay,
				afterReplay,
				"confirmed negative replay preserves owner, all queue rows and pacing",
			)
			t.Logf(
				"expired original cooldown=%s; replay before=%s after=%s actual primary wires=%d",
				cooldown,
				beforeReplay,
				afterReplay,
				loss.count(),
			)
			assert.Equal(t, 4, loss.count(), "completion replay makes no provider call")
			r.postAttempt(t, "begin", delivery.Attempt{ID: r.first, Generation: prepared.Attempt}, http.StatusOK)
			terminal := r.status(t, r.first)
			assert.Equal(t, "failed", terminal.State)
			assert.Equal(t, "telegram_uncertain_retry_exhausted", terminal.Reason)
			assert.Equal(t, deferred.Attempt, terminal.Attempt, "exhaustion admits no generation")
			assert.Equal(t, int64(3), terminal.UncertainResends)
			assert.Equal(t, original.LastUncertainAttempt, terminal.LastUncertainAttempt)
			assert.Equal(t, original.LastUncertainReason, terminal.LastUncertainReason)
			assert.Equal(t, original.LastUncertainRecordedAt, terminal.LastUncertainRecordedAt)
			assert.Equal(t, 4, loss.count(), "no fourth additional wire")
			assert.Zero(t, terminal.MessageID, "confirmed negative responses cannot fabricate a receipt")
			require.NoError(t, dispatch(t.Context(), r.second))
			assert.Equal(t, "sent", r.status(t, r.second).State)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, terminal, r.status(t, r.first), "terminal policy does not revive")
			assert.JSONEq(t, business, notificationBusinessSnapshot(t, r))
		})
	}
}

func TestNotificationUncertainRetryFinal429UsesOnlyActualCooldown(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			business := notificationBusinessSnapshot(t, r)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			original := r.status(t, r.first)
			wire := notificationWireSnapshot(t, r)
			require.NotNil(t, wire)
			provider := notificationCooldownProviderWithRetryAfter(t, r, wire.Text, 1)
			r.f.b.TG.Base = provider.URL
			for attempt := range 3 {
				waitNotificationEligibility(t, r, r.status(t, r.first).AvailableAt)
				require.NoError(t, dispatch(t.Context(), r.first))
				state := r.status(t, r.first)
				require.Equal(t, "pending", state.State)
				require.Equal(t, int64(attempt+1), state.UncertainResends)
				assertNotification429Projection(t, r, domain, state)
			}
			deferred := r.status(t, r.first)
			var cooldown time.Time
			require.NoError(t, r.f.db.QueryRow(t.Context(),
				"SELECT max(not_before) FROM core.delivery_pacing WHERE bot_id=$1", syntheticDeliverySettings().BotID,
			).Scan(&cooldown))
			assert.WithinDuration(t, cooldown, deferred.AvailableAt, time.Millisecond,
				"exhausted retry budget adds no uncertainty backoff beyond the actual provider cooldown")
			require.NoError(t, dispatch(t.Context(), r.first))
			require.NoError(t, dispatch(t.Context(), r.second))
			assert.Equal(t, 4, loss.count(), "cooldown admits no extra wire")
			waitNotificationEligibility(t, r, cooldown)
			require.NoError(t, dispatch(t.Context(), r.first))
			terminal := r.status(t, r.first)
			assert.Equal(t, "failed", terminal.State)
			assert.Equal(t, "telegram_uncertain_retry_exhausted", terminal.Reason)
			assert.Equal(t, deferred.Attempt, terminal.Attempt, "exhaustion admits no generation")
			assert.Equal(t, int64(3), terminal.UncertainResends)
			assert.Equal(t, original.LastUncertainAttempt, terminal.LastUncertainAttempt)
			assert.Equal(t, original.LastUncertainReason, terminal.LastUncertainReason)
			assert.Equal(t, original.LastUncertainRecordedAt, terminal.LastUncertainRecordedAt)
			assert.Zero(t, terminal.MessageID)
			assert.Equal(t, 4, loss.count(), "no fourth additional wire")
			require.NoError(t, dispatch(t.Context(), r.second))
			assert.Equal(t, "sent", r.status(t, r.second).State, "follower proceeds after actual cooldown")
			assert.Equal(t, wire, notificationWireSnapshot(t, r))
			assert.JSONEq(t, business, notificationBusinessSnapshot(t, r))
		})
	}
}

func TestNotificationUncertainRetryExhaustionObservesIndependentDeadlines(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		for _, boundary := range []string{"after_final429", "after_preparation", "queue_deadline"} {
			t.Run(domain+"/"+boundary, func(t *testing.T) {
				t.Parallel()
				r := notificationRuntime(t, domain)
				loss := exhaustibleNotification429(t, r, domain)
				before := r.status(t, r.first)
				waitNotificationEligibility(t, r, before.AvailableAt)
				if boundary == "after_preparation" {
					assertNotificationCandidate(t, r, domain)
					r.prepare(t)
				}
				deadline := extendIndependentNotificationDeadline(t, r, domain, boundary)
				pacing := notificationPacingAndFairness(t, r)
				if boundary != "after_preparation" {
					r.prepare(t)
				}
				prepared := r.status(t, r.first)
				r.postAttempt(t, "begin", delivery.Attempt{ID: r.first, Generation: prepared.Attempt}, http.StatusOK)
				current := r.status(t, r.first)
				require.Equal(t, "pending", current.State, "independent deadline delays exhaustion")
				assert.WithinDuration(t, deadline, current.AvailableAt, time.Millisecond)
				assert.Equal(t, before.Attempt, current.Attempt)
				assert.Equal(t, before.UncertainResends, current.UncertainResends)
				assert.Equal(t, before.LastUncertainAttempt, current.LastUncertainAttempt)
				assertNotification429Projection(t, r, domain, current)
				assert.JSONEq(t, pacing, notificationPacingAndFairness(t, r), "observation reserves nothing")
				assert.Equal(t, 4, loss.count())
				assertNotificationFinal429ReplayInert(t, r, current.Attempt)
				waitNotificationEligibility(t, r, deadline)
				dispatch := exactNotificationDelivery(r, domain)
				require.NoError(t, dispatch(t.Context(), r.first))
				terminal := r.status(t, r.first)
				assert.Equal(t, "failed", terminal.State)
				assert.Equal(t, "telegram_uncertain_retry_exhausted", terminal.Reason)
				assert.Equal(t, before.Attempt, terminal.Attempt)
				assert.Equal(t, int64(3), terminal.UncertainResends)
				assert.JSONEq(t, pacing, notificationPacingAndFairness(t, r))
				assert.Equal(t, 4, loss.count(), "no fourth additional wire")
				require.NoError(t, dispatch(t.Context(), r.second))
				assert.Equal(t, "sent", r.status(t, r.second).State)
			})
		}
	}
}

func assertNotificationFinal429ReplayInert(t *testing.T, r *notificationRuntimeFixture, attempt int64) {
	t.Helper()
	before := notificationDeliverySnapshot(t, r)
	r.postAttempt(t, "complete", map[string]any{
		"id": r.first, "attempt": attempt,
		"outcome": delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 1},
	}, http.StatusOK)
	assert.JSONEq(
		t,
		before,
		notificationDeliverySnapshot(t, r),
		"final canonical negative replay stays inert after observation",
	)
}

func exhaustibleNotification429(t *testing.T, r *notificationRuntimeFixture, domain string) *notificationLostResponse {
	t.Helper()
	loss := &notificationLostResponse{drops: 1}
	r.f.b.TG.HTTP = &http.Client{Transport: loss}
	dispatch := exactNotificationDelivery(r, domain)
	require.NoError(t, dispatch(t.Context(), r.first))
	requireNotificationAccepted(t, r, loss)
	wire := notificationWireSnapshot(t, r)
	require.NotNil(t, wire)
	provider := notificationCooldownProviderWithRetryAfter(t, r, wire.Text, 1)
	r.f.b.TG.Base = provider.URL
	for attempt := range 3 {
		// Prior backoffs are covered separately. Expire only preparatory clocks;
		// retain the actual final 429 and all subsequent independent deadlines.
		r.wake(t)
		require.NoError(t, dispatch(t.Context(), r.first))
		state := r.status(t, r.first)
		require.Equal(t, int64(attempt+1), state.UncertainResends)
		assertNotification429Projection(t, r, domain, state)
	}
	return loss
}

func TestNotificationUncertainRetryExhaustionCancelsBeforeIndependentDeadline(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := exhaustibleNotification429(t, r, domain)
			before := r.status(t, r.first)
			waitNotificationEligibility(t, r, before.AvailableAt)
			r.prepare(t)
			deadline := extendIndependentNotificationDeadline(t, r, domain, "after_preparation")
			_, err := r.f.db.Exec(t.Context(), "UPDATE core.users SET can_book=false WHERE id='bob'")
			require.NoError(t, err)
			pacing := notificationPacingAndFairness(t, r)
			r.postAttempt(t, "begin", delivery.Attempt{ID: r.first, Generation: before.Attempt}, http.StatusOK)
			current := r.status(t, r.first)
			assert.Equal(t, "cancelled", current.State, "current rights cancellation does not wait for pacing")
			assert.Equal(t, "notification_no_longer_current", current.Reason)
			assert.Equal(t, before.Attempt, current.Attempt)
			assert.Equal(t, int64(3), current.UncertainResends)
			assert.JSONEq(t, pacing, notificationPacingAndFairness(t, r))
			assert.Equal(t, 4, loss.count())
			var now time.Time
			require.NoError(t, r.f.db.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&now))
			assert.True(t, now.Before(deadline), "cancellation commits before independent deadline")
		})
	}
}

func assertNotificationCandidate(t *testing.T, r *notificationRuntimeFixture, domain string) {
	t.Helper()
	for _, entry := range botDeliveryCandidates(t, r.f.b) {
		if entry.Reference.Owner == notificationQueueOwner(domain) &&
			entry.Reference.Key == strconv.FormatInt(r.first, 10) {
			return
		}
	}
	t.Fatal("exhausted owner missing from advisory candidates before independent extension")
}

func extendIndependentNotificationDeadline(
	t *testing.T,
	r *notificationRuntimeFixture,
	domain, boundary string,
) time.Time {
	t.Helper()
	if boundary == "queue_deadline" {
		var deadline time.Time
		err := r.f.db.QueryRow(t.Context(), `UPDATE core.delivery_queue SET not_before=clock_timestamp()+interval '2 seconds'
 WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key='send' RETURNING not_before`,
			syntheticDeliverySettings().BotID, string(notificationQueueOwner(domain)), strconv.FormatInt(r.first, 10)).
			Scan(&deadline)
		require.NoError(t, err)
		return deadline
	}
	// A different chat's canonical rate limit extends shared bot pacing.
	tx, err := r.f.db.Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, deadline, err := delivery.Schedule(
		t.Context(),
		tx,
		syntheticDeliverySettings(),
		delivery.Destination{Chat: "303"},
		delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 2},
	)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	return deadline
}

func notificationPacingAndFairness(t *testing.T, r *notificationRuntimeFixture) string {
	t.Helper()
	var raw []byte
	require.NoError(t, r.f.db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'pacing',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.bot_id,p.chat) FROM core.delivery_pacing p),
 'fairness',(SELECT jsonb_agg(to_jsonb(f) ORDER BY f.bot_id) FROM core.delivery_fairness f),
 'lanes',(SELECT jsonb_agg(to_jsonb(l) ORDER BY l.bot_id,l.chat) FROM core.delivery_lanes l))`).Scan(&raw))
	return string(raw)
}

func assertNotification429Projection(
	t *testing.T, r *notificationRuntimeFixture, domain string, state notificationRuntimeStatus,
) {
	t.Helper()
	var queueState string
	var confirmed int64
	var deadline time.Time
	require.NoError(t, r.f.db.QueryRow(t.Context(),
		`SELECT state,not_before FROM core.delivery_queue
 WHERE bot_id=$1 AND owner_kind=$2 AND owner_key=$3 AND effect_key='send'`,
		syntheticDeliverySettings().BotID, string(notificationQueueOwner(domain)), strconv.FormatInt(r.first, 10),
	).Scan(&queueState, &deadline))
	assert.Equal(t, "pending", queueState, "the shared lane retains the deferred intent")
	assert.WithinDuration(t, state.AvailableAt, deadline, 0)
	require.NoError(t, r.f.db.QueryRow(t.Context(),
		"SELECT last_confirmed_attempt FROM "+r.table+" WHERE id=$1", r.first,
	).Scan(&confirmed))
	assert.Equal(t, state.Attempt, confirmed, "each actual 429 is a confirmed negative wire outcome")
	assert.Zero(t, state.MessageID)
}

func notificationCooldownProvider(t *testing.T, r *notificationRuntimeFixture, text string) *httptest.Server {
	t.Helper()
	return notificationCooldownProviderWithRetryAfter(t, r, text, 21)
}

func notificationCooldownProviderWithRetryAfter(
	t *testing.T, r *notificationRuntimeFixture, text string, retryAfter int64,
) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	var negatives int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(w, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		_ = request.Body.Close()
		request.Body = io.NopCloser(bytes.NewReader(body))
		var message telegram.Send
		if json.Unmarshal(body, &message) != nil {
			http.Error(w, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		reject := message.ChatID == 202 && message.Text == text && negatives < 3
		if reject {
			negatives++
		}
		negative := negatives
		mu.Unlock()
		if !reject {
			r.f.fake.Config.Handler.ServeHTTP(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		t.Logf("actual provider HTTP 429 retry_after=%d; primary negative=%d", retryAfter, negative)
		_, _ = fmt.Fprintf(
			w,
			`{"ok":false,"error_code":429,"description":"synthetic confirmed cooldown","parameters":{"retry_after":%d}}`,
			retryAfter,
		)
	}))
	t.Cleanup(server.Close)
	return server
}

// Wait for the source clock; never shorten the persisted provider deadline.
func waitNotificationEligibility(t *testing.T, r *notificationRuntimeFixture, deadline time.Time) {
	t.Helper()
	for {
		var now time.Time
		require.NoError(t, r.f.db.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&now))
		if !now.Before(deadline) {
			return
		}
		timer := time.NewTimer(min(deadline.Sub(now), time.Second))
		select {
		case <-timer.C:
		case <-t.Context().Done():
			timer.Stop()
			t.Fatal("notification source clock did not reach eligibility")
		}
	}
}

func TestNotificationUncertainRetryPrewirePacingKeepsBudget(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			original := r.status(t, r.first)
			r.wake(t)
			_, err := r.f.db.Exec(
				t.Context(),
				"UPDATE core.delivery_pacing SET not_before=clock_timestamp()+interval '1 minute'",
			)
			require.NoError(t, err)
			require.NoError(t, dispatch(t.Context(), r.first))
			current := r.status(t, r.first)
			assert.Equal(t, "pending", current.State)
			assert.Zero(t, current.UncertainResends)
			assert.Equal(t, original.LastUncertainAttempt, current.LastUncertainAttempt)
			assert.Equal(t, 1, loss.count())
			deadline := current.AvailableAt
			require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
			assert.Equal(t, deadline, r.status(t, r.first).AvailableAt)
			r.wake(t)
			require.NoError(t, dispatch(t.Context(), r.first))
			assert.Equal(t, "sent", r.status(t, r.first).State)
			assert.Equal(t, int64(1), r.status(t, r.first).UncertainResends)
			assert.Equal(t, 2, loss.count())
		})
	}
}
func notificationBusinessSnapshot(t *testing.T, r *notificationRuntimeFixture) string {
	t.Helper()
	var raw []byte
	require.NoError(t, r.f.db.QueryRow(t.Context(), `SELECT jsonb_build_object(
 'orders',(SELECT jsonb_agg(to_jsonb(v) ORDER BY v.id) FROM core.orders v),
 'passes',(SELECT jsonb_agg(to_jsonb(v) ORDER BY v.event_id,v.owner) FROM core.pass_bookings v),
 'massage',(SELECT jsonb_agg(to_jsonb(v) ORDER BY v.id) FROM core.massage_bookings v),
 'food',(SELECT jsonb_agg(to_jsonb(v) ORDER BY v.id) FROM core.food_orders v),
 'food_payments',(SELECT jsonb_agg(to_jsonb(v) ORDER BY v.order_id,v.kind,v.generation) FROM core.food_payments v))`).Scan(&raw))
	return string(raw)
}

func heldNotification429(t *testing.T) (string, <-chan telegram.Send, func()) {
	t.Helper()
	arrived := make(chan telegram.Send, 1)
	unblock := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(unblock) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var send telegram.Send
		if json.NewDecoder(request.Body).Decode(&send) != nil {
			http.Error(w, "invalid synthetic request", http.StatusBadRequest)
			return
		}
		select {
		case arrived <- send:
		case <-request.Context().Done():
			return
		}
		select {
		case <-unblock:
		case <-request.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(
			w,
			`{"ok":false,"error_code":429,"description":"synthetic confirmed cooldown","parameters":{"retry_after":60}}`,
		)
	}))
	t.Cleanup(func() { release(); server.Close() })
	return server.URL, arrived, release
}

func TestNotificationUncertainRetryKnown429FencesContradictoryReceipt(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		for _, policy := range []string{"pending", "cancelled", "cancelled_before_response"} {
			t.Run(domain+"/"+policy, func(t *testing.T) {
				t.Parallel()
				r := notificationRuntime(t, domain)
				url, arrived, release := heldNotification429(t)
				r.f.b.TG.Base = url
				finished := make(chan error, 1)
				go func() { finished <- exactNotificationDelivery(r, domain)(t.Context(), r.first) }()
				var request telegram.Send
				select {
				case request = <-arrived:
				case <-t.Context().Done():
					t.Fatal("admitted wire did not reach held synthetic provider")
				}
				require.NotEmpty(t, request.Text)
				admitted := r.status(t, r.first)
				require.Equal(t, "sending", admitted.State)
				command, err := r.f.db.Exec(t.Context(), "UPDATE "+r.table+
					" SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1 AND delivery_attempt=$2 AND delivery_state='sending'",
					r.first, admitted.Attempt)
				require.NoError(t, err)
				require.EqualValues(t, 1, command.RowsAffected())
				require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
				recovered := r.status(t, r.first)
				require.Equal(t, "pending", recovered.State)
				require.Equal(t, admitted.Attempt, recovered.Attempt)
				require.Equal(t, admitted.Attempt, recovered.LastUncertainAttempt)
				var confirmed *int64
				require.NoError(
					t,
					r.f.db.QueryRow(t.Context(), "SELECT last_confirmed_attempt FROM "+r.table+" WHERE id=$1", r.first).
						Scan(&confirmed),
				)
				require.Nil(t, confirmed, "recovery records uncertainty, not a confirmed provider response")
				var terminalBefore map[string]any
				if policy == "cancelled_before_response" {
					r.postAttempt(t, "complete", map[string]any{
						"id": r.first, "attempt": admitted.Attempt,
						"outcome": delivery.Outcome{Kind: delivery.Cancelled, Reason: "synthetic_policy_cancel"},
					}, http.StatusOK)
					terminalBefore = notificationReceiptSnapshot(t, r)
				}
				release()
				require.NoError(
					t,
					<-finished,
					"confirmed late negative must be recorded without changing terminal policy",
				)
				known := r.status(t, r.first)
				if policy == "cancelled_before_response" {
					after := notificationReceiptSnapshot(t, r)
					delete(terminalBefore, "last_confirmed_attempt")
					delete(after, "last_confirmed_attempt")
					assert.Equal(t, terminalBefore, after, "confirmed negative must change only its metadata fence")
					require.Equal(t, "cancelled", known.State)
				} else {
					require.Equal(t, "telegram_rate_limit", known.Reason,
						"the real held HTTP response must be classified and committed")
				}
				require.Equal(t, recovered.LastUncertainAttempt, known.LastUncertainAttempt)
				require.NoError(
					t,
					r.f.db.QueryRow(t.Context(), "SELECT last_confirmed_attempt FROM "+r.table+" WHERE id=$1", r.first).
						Scan(&confirmed),
				)
				require.NotNil(t, confirmed)
				require.Equal(t, admitted.Attempt, *confirmed)
				if policy == "cancelled" {
					r.postAttempt(t, "complete", map[string]any{
						"id": r.first, "attempt": admitted.Attempt,
						"outcome": delivery.Outcome{Kind: delivery.Cancelled, Reason: "synthetic_policy_cancel"},
					}, http.StatusOK)
				}
				before := notificationReceiptSnapshot(t, r)
				// This rejected input contradicts the confirmed 429; it is not a delivered receipt.
				r.postAttempt(t, "complete", map[string]any{
					"id": r.first, "attempt": admitted.Attempt, "text": request.Text,
					"outcome": delivery.Outcome{Kind: delivery.Succeeded, MessageID: 987654},
				}, http.StatusConflict)
				assert.Equal(t, before, notificationReceiptSnapshot(t, r))
			})
		}
	}
}

func TestNotificationUncertainRetryRejectsCorruptCapturedKeyboard(t *testing.T) {
	t.Parallel()
	for name, markup := range map[string]string{
		"string":       `{"inline_keyboard":"invalid"}`,
		"null button":  `{"inline_keyboard":[[null]]}`,
		"empty button": `{"inline_keyboard":[[{}]]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, domain := range []string{"orders", "registration", "massage", "food"} {
				t.Run(domain, func(t *testing.T) {
					t.Parallel()
					r := notificationRuntime(t, domain)
					loss := &notificationLostResponse{drops: 1}
					r.f.b.TG.HTTP = &http.Client{Transport: loss}
					dispatch := exactNotificationDelivery(r, domain)
					require.NoError(t, dispatch(t.Context(), r.first))
					requireNotificationAccepted(t, r, loss)
					original := r.status(t, r.first)
					_, err := r.f.db.Exec(
						t.Context(),
						"UPDATE "+r.table+" SET delivery_wire_payload=jsonb_set(delivery_wire_payload,'{markup}',$2::jsonb) WHERE id=$1",
						r.first,
						markup,
					)
					require.NoError(t, err)
					var captured []byte
					require.NoError(
						t,
						r.f.db.QueryRow(t.Context(), "SELECT delivery_wire_payload FROM "+r.table+" WHERE id=$1", r.first).
							Scan(&captured),
					)
					r.wake(t)
					require.Error(t, dispatch(t.Context(), r.first))
					actual := r.status(t, r.first)
					assert.Equal(t, "pending", actual.State, "invalid captured markup must fail before send admission")
					assert.Equal(
						t,
						original.UncertainResends,
						actual.UncertainResends,
						"prewire failure has zero budget charge",
					)
					assert.Len(t, loss.payloads(), 1, "only the original real accepted HTTP wire occurred")
					assert.Equal(t, original.LastUncertainAttempt, actual.LastUncertainAttempt)
					var after []byte
					require.NoError(
						t,
						r.f.db.QueryRow(t.Context(), "SELECT delivery_wire_payload FROM "+r.table+" WHERE id=$1", r.first).
							Scan(&after),
					)
					assert.Equal(t, captured, after)
				})
			}
		})
	}
}

func TestNotificationUncertainRetryCustomNormalizationCannotConfirm(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			r.prepare(t)
			prepared := r.status(t, r.first)
			r.postAttempt(t, "begin", delivery.Attempt{ID: r.first, Generation: prepared.Attempt}, http.StatusOK)
			admitted := r.status(t, r.first)
			require.Equal(t, "sending", admitted.State)
			outcome := delivery.Outcome{
				Kind:       delivery.Deferred,
				Reason:     "synthetic_custom_deferred",
				RetryAfter: 1 << 62,
			}
			require.True(t, outcome.Valid())
			r.postAttempt(
				t,
				"complete",
				map[string]any{"id": r.first, "attempt": admitted.Attempt, "outcome": outcome},
				http.StatusOK,
			)
			require.Equal(t, "parked", r.status(t, r.first).State)
			var confirmed *int64
			require.NoError(
				t,
				r.f.db.QueryRow(t.Context(), "SELECT last_confirmed_attempt FROM "+r.table+" WHERE id=$1", r.first).
					Scan(&confirmed),
			)
			assert.Nil(t, confirmed, "normalized custom scheduling is not canonical wire proof")
		})
	}
}

func TestNotificationUncertainRetryKnown429RendersCurrentUntilUnknown(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			setNotificationLanguage(t, r, "en")
			provider, arrived, release := heldNotification429(t)
			release()
			r.f.b.TG.Base = provider
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			request := <-arrived
			require.NotEmpty(t, request.Text)
			known := r.status(t, r.first)
			require.Equal(t, "telegram_rate_limit", known.Reason)
			require.Zero(t, known.LastUncertainAttempt)
			initial := notificationWireSnapshot(t, r)
			require.NotNil(t, initial)
			setNotificationLanguage(t, r, "ru")
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.Base = r.f.fake.URL
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			r.wake(t)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			current := loss.payloads()
			require.Len(t, current, 1)
			assert.NotEqual(t, initial.Text, current[0].Text, "ordinary known 429 must render current locale")
			captured := notificationWireSnapshot(t, r)
			require.NotNil(t, captured)
			assert.Equal(t, current[0].Text, captured.Text, "unknown freezes the newly admitted wire")
			setNotificationLanguage(t, r, "en")
			r.wake(t)
			require.NoError(t, dispatch(t.Context(), r.first))
			after := loss.payloads()
			require.Len(t, after, 2)
			assert.Equal(t, current[0], after[1], "retry after unknown preserves exact latest admitted wire")
			assert.Equal(t, int64(1), r.status(t, r.first).UncertainResends)
		})
	}
}

func TestNotificationUncertainRetryConfirmedNegativeAfterPreflight(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		t.Run(domain, func(t *testing.T) {
			t.Parallel()
			r := notificationRuntime(t, domain)
			loss := &notificationLostResponse{drops: 1}
			r.f.b.TG.HTTP = &http.Client{Transport: loss}
			dispatch := exactNotificationDelivery(r, domain)
			require.NoError(t, dispatch(t.Context(), r.first))
			requireNotificationAccepted(t, r, loss)
			wire := notificationWireSnapshot(t, r)
			require.NotNil(t, wire)
			provider := notificationCooldownProviderWithRetryAfter(t, r, wire.Text, 1)
			r.f.b.TG.Base = provider.URL
			// Advance only this isolated fixture's clocks; every retry still crosses Begin and Telegram.
			for range 3 {
				r.wake(t)
				require.NoError(t, dispatch(t.Context(), r.first))
			}
			confirmed := r.status(t, r.first)
			require.EqualValues(t, 3, confirmed.UncertainResends)
			require.Equal(t, "telegram_rate_limit", confirmed.Reason)
			r.wake(t)
			original := r.f.b.API
			preflight := &notificationFollowupProvider{token: original.SandboxToken}
			r.f.b.API.Links, r.f.b.API.Exchange = preflight, preflight
			require.NoError(t, dispatch(t.Context(), r.first))
			require.Positive(t, preflight.calls, "actual identity lookup fails before Begin")
			deferred := r.status(t, r.first)
			require.Equal(t, confirmed.Attempt, deferred.Attempt)
			require.Equal(t, "notification_preflight_unavailable", deferred.Reason)
			require.Equal(t, 4, loss.count(), "preflight does not create a wire")
			before := notificationDeliverySnapshot(t, r)
			r.postAttempt(t, "complete", map[string]any{
				"id": r.first, "attempt": confirmed.Attempt,
				"outcome": delivery.Outcome{Kind: delivery.Deferred, Reason: "telegram_rate_limit", RetryAfter: 1},
			}, http.StatusOK)
			assert.Equal(
				t,
				before,
				notificationDeliverySnapshot(t, r),
				"confirmed replay preserves owners, followers, queue and pacing after preflight",
			)
			assert.Equal(t, 4, loss.count())
		})
	}
}

func TestNotificationUncertainRetryHeldSuccessAfterRecoveryRevocation(t *testing.T) {
	t.Parallel()
	for _, domain := range []string{"orders", "registration", "massage", "food"} {
		for _, revoke := range []string{"rights", "source"} {
			t.Run(domain+"/"+revoke, func(t *testing.T) {
				t.Parallel()
				notificationHeldSuccessRevocation(t, domain, revoke)
			})
		}
	}
}

func notificationHeldSuccessRevocation(t *testing.T, domain, revoke string) {
	t.Helper()
	r := notificationRuntime(t, domain)
	if domain == "food" && revoke == "source" {
		r = foodReviewNotificationRuntime(t)
	}
	accepted := make(chan telegram.Message, 1)
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	r.f.b.TG.HTTP = notificationHeldSuccessClient(accepted, release)
	done := make(chan error, 1)
	go func() { done <- exactNotificationDelivery(r, domain)(t.Context(), r.first) }()
	var message telegram.Message
	select {
	case message = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("real provider did not accept the held notification")
	}
	require.Positive(t, message.ID)
	sending := r.status(t, r.first)
	require.Equal(t, "sending", sending.State)
	_, err := r.f.db.Exec(
		t.Context(),
		"UPDATE "+r.table+" SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1",
		r.first,
	)
	require.NoError(t, err)
	require.NoError(t, recoverNotificationDelivery(r, domain)(t.Context()))
	recovered := r.status(t, r.first)
	require.Equal(t, "pending", recovered.State)
	require.Equal(t, sending.Attempt, recovered.LastUncertainAttempt)
	err = revokeRecoveredNotification(t, r, domain, revoke)
	require.NoError(t, err)
	pacing := notificationPacingAndFairness(t, r)
	unblock()
	select {
	case err = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("held notification completion did not return")
	}
	require.Error(t, err, "revoked completion conflicts before adapter follow-up")
	after := r.status(t, r.first)
	assert.Equal(t, "cancelled", after.State)
	assert.Equal(t, message.ID, after.MessageID, "retain the genuine accepted receipt")
	assert.False(t, after.FollowupPending)
	assert.Equal(t, sending.Attempt, after.Attempt)
	assert.Equal(t, recovered.UncertainResends, after.UncertainResends)
	assert.JSONEq(t, pacing, notificationPacingAndFairness(t, r))
	r.postAttempt(t, "complete", map[string]any{
		"id": r.first, "attempt": sending.Attempt, "text": message.Text,
		"outcome": delivery.Outcome{Kind: delivery.Succeeded, MessageID: message.ID},
	}, http.StatusOK)
	assert.Equal(t, after, r.status(t, r.first), "terminal receipt replay remains metadata only")
}

func notificationHeldSuccessClient(accepted chan<- telegram.Message, release <-chan struct{}) *http.Client {
	return &http.Client{
		Transport: passDeliveryTransport(func(request *http.Request) (*http.Response, error) {
			response, err := http.DefaultTransport.RoundTrip(request)
			if err != nil || !strings.HasSuffix(request.URL.Path, "/sendMessage") {
				return response, err
			}
			raw, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				return nil, err
			}
			response.Body = io.NopCloser(bytes.NewReader(raw))
			var result struct {
				Result telegram.Message `json:"result"`
			}
			if err = json.Unmarshal(raw, &result); err != nil {
				return nil, err
			}
			accepted <- result.Result
			select {
			case <-release:
				return response, nil
			case <-request.Context().Done():
				return nil, request.Context().Err()
			}
		}),
	}
}

func revokeRecoveredNotification(t *testing.T, r *notificationRuntimeFixture, domain, revoke string) error {
	t.Helper()
	var err error
	if revoke == "rights" {
		_, err = r.f.db.Exec(t.Context(), "UPDATE core.users SET can_book=false WHERE id='bob'")
	} else {
		switch domain {
		case "orders":
			_, err = r.f.db.Exec(t.Context(), "DELETE FROM core.order_admins WHERE owner='bob'")
		case "registration":
			_, err = r.f.db.Exec(
				t.Context(),
				"UPDATE core.pass_bookings SET created_at=created_at+interval '1 second',assigned_at=assigned_at+interval '1 second' WHERE event_id='dance' AND owner='bob'",
			)
		case "massage":
			_, err = r.f.db.Exec(
				t.Context(),
				"UPDATE core.massage_specialists SET notify_bookings=false WHERE owner='bob'",
			)
		case "food":
			_, err = r.f.db.Exec(
				t.Context(),
				"UPDATE core.food_admins SET can_review=false WHERE owner='bob'",
			)
		}
	}
	return err
}
