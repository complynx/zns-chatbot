package integration_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The sink receives the real message before the adapter loses its response.
type notificationLostResponse struct {
	mu    sync.Mutex
	text  string
	calls int
	drops int
	wires []telegram.Send
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

func TestNotificationUncertainRetryPreservesWireAfterLanguageChange(t *testing.T) {
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
			require.Equal(t, "pending", r.status(t, r.first).State)
			before := loss.payloads()
			require.Len(t, before, 1)
			if domain == "massage" {
				require.NotEmpty(t, before[0].Markup.Rows)
			}
			_, err := r.f.db.Exec(t.Context(), `UPDATE core.users SET language='ru' WHERE id='bob'`)
			require.NoError(t, err)
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
			assert.GreaterOrEqual(t, time.Until(first.AvailableAt), r.f.b.Delivery.Fallback-time.Second)
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
					r.f.b.Delivery.Fallback*time.Duration(1<<attempt)-time.Second)
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
