package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func notificationList(t *testing.T, f paymentFixture) []db.NotificationSummary {
	t.Helper()
	code, data := agentAdminRead(t, f.H, f.Admin, "notifications", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(data))
	var result struct {
		Result struct {
			Notifications []db.NotificationSummary `json:"notifications"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	return result.Result.Notifications
}
func notificationDetail(t *testing.T, f paymentFixture, id uuid.UUID) db.PaymentNotification {
	t.Helper()
	code, data := agentAdminRead(t, f.H, f.Admin, "notification", map[string]any{"id": id})
	require.Equal(t, http.StatusOK, code, string(data))
	var result struct {
		Result db.PaymentNotification `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	return result.Result
}
func awaitNotification(t *testing.T, f paymentFixture, id uuid.UUID, state string) db.PaymentNotification {
	t.Helper()
	var result db.PaymentNotification
	require.Eventually(
		t,
		func() bool { result = notificationDetail(t, f, id); return result.State == state },
		8*time.Second,
		25*time.Millisecond,
	)
	return result
}
func sentNotifications(p *peers, chatID int64) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var result []string
	for _, message := range p.sent {
		if message.ChatID == chatID {
			result = append(result, message.Text)
		}
	}
	return result
}
func pendingNotice(t *testing.T, f paymentFixture, operation string) (payments.Evidence, uuid.UUID) {
	t.Helper()
	file := paymentFile(t, f.H, f.OwnerID, 93001, "notification-proof", time.Now().UTC().Truncate(time.Second))
	evidence := submittedPayment(t, f, operation, file, 3087, payments.Initial)
	rows := notificationList(t, f)
	require.Len(t, rows, 1)
	require.Equal(t, evidence.ID, rows[0].EvidenceID)
	require.Equal(t, f.AmbassadorID, rows[0].RecipientID)
	require.Equal(t, "submitted", rows[0].Kind)
	return evidence, rows[0].ID
}

func TestPaymentNotificationsResumeAndDistinguishDepositFromFullPayment(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f := setupPayment(t, p)
	f.H.stop(t)
	evidence, id := pendingNotice(t, f, "notice-submit")
	require.Equal(t, "pending", notificationDetail(t, f, id).State)
	code, data := paymentRequest(
		t,
		f,
		f.Owner,
		"submit",
		"notice-submit",
		payments.Submission{
			EventID:       f.Event.ID,
			PassID:        f.Pass.ID,
			FileID:        evidence.FileID,
			AmountMinor:   3087,
			Currency:      "BYN",
			CurrencyScale: 2,
			Purpose:       payments.Initial,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Len(t, notificationList(t, f), 1)
	_, err := f.H.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_users SET profile=$2 WHERE id=ANY($1::uuid[])",
			[]uuid.UUID{f.OwnerID, f.AmbassadorID}, json.RawMessage(`{"language_code":"be"}`))
	require.NoError(t, err)
	f.H.start(t)
	delivered := awaitNotification(t, f, id, "delivered")
	require.Contains(t, string(delivered.Receipt), `"message_id"`)
	messages := sentNotifications(p, 11003)
	require.Len(t, messages, 1)
	require.Contains(t, messages[0], "Оплата ещё не подтверждена")
	require.Contains(t, messages[0], "30.87 BYN")
	require.Contains(t, messages[0], "Europe/Minsk")
	require.Contains(t, messages[0], evidence.ID.String())
	code, data = paymentRequest(
		t,
		f,
		f.Owner,
		"balance",
		"notice-balance-before",
		map[string]any{"event_id": f.Event.ID, "pass_id": f.Pass.ID},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"confirmed_minor":0`)
	body := preparedPayment(
		t,
		f,
		f.Ambassador,
		"notice-review",
		payments.ReviewChange{
			EventID:     f.Event.ID,
			EvidenceID:  evidence.ID,
			Decision:    payments.Confirmed,
			AmountMinor: 3087,
			Reason:      "Checked by ambassador",
		},
	)
	result := executePayment(t, f, f.Ambassador, body)
	require.True(t, result.Balance.DepositSatisfied)
	require.Equal(t, result, executePayment(t, f, f.Ambassador, body))
	rows := notificationList(t, f)
	require.Len(t, rows, 2)
	var ownerNotice uuid.UUID
	for _, row := range rows {
		if row.Kind == payments.Confirmed {
			ownerNotice = row.ID
		}
	}
	require.NotEqual(t, uuid.Nil, ownerNotice)
	awaitNotification(t, f, ownerNotice, "delivered")
	ownerMessages := sentNotifications(p, 11002)
	require.Len(t, ownerMessages, 1)
	require.Contains(t, ownerMessages[0], "Предоплата подтверждена")
	require.Contains(t, ownerMessages[0], "Состояние сразу после этого решения")
	zone, err := time.LoadLocation(f.Event.Document.Timezone)
	require.NoError(t, err)
	require.Contains(t, ownerMessages[0], result.Review.ReviewedAt.In(zone).Format(time.RFC3339))
	require.Contains(t, ownerMessages[0], "не оплачен полностью")
	require.Contains(t, ownerMessages[0], "92.58 BYN")
	require.NotContains(t, ownerMessages[0], "Пасс оплачен полностью")
	code, _ = agentAdminRead(t, f.H, f.Owner, "notifications", map[string]any{})
	require.Equal(t, http.StatusForbidden, code)
	code, _ = agentAdminRead(t, f.H, f.Ambassador, "notification", map[string]any{"id": id})
	require.Equal(t, http.StatusForbidden, code)
	f.H.stop(t)
	f.H.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 11002)) > 1 || len(sentNotifications(p, 11003)) > 1 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
	p.mu.Lock()
	require.Empty(t, p.createKeys)
	p.mu.Unlock()
}

func TestPaymentNotificationLostTelegramReceiptDoesNotResendAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f := setupPayment(t, p)
	f.H.stop(t)
	_, id := pendingNotice(t, f, "unknown-notice")
	p.mu.Lock()
	p.loseSend = true
	p.mu.Unlock()
	f.H.start(t)
	awaitNotification(t, f, id, "delivery_unknown")
	require.Len(t, sentNotifications(p, 11003), 1)
	f.H.stop(t)
	f.H.start(t)
	require.Equal(t, "delivery_unknown", notificationDetail(t, f, id).State)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 11003)) > 1 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
}

func TestPaymentNotificationRevocationBeforeDeliveryBlocksDisclosure(t *testing.T) {
	t.Parallel()
	for _, source := range []bool{false, true} {
		t.Run(map[bool]string{false: "recipient_event_role", true: "source_account"}[source], func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			f := setupPayment(t, p)
			f.H.stop(t)
			_, id := pendingNotice(t, f, "revoked-notice")
			if source {
				require.NoError(t, f.H.store.RevokeUser(context.Background(), f.OwnerID, true))
			} else {
				operator := paymentScope(t, f.H, f.AdminID, true)
				_, err := f.H.store.ChangeAuthority(
					context.Background(),
					operator.EventScope,
					operator.Subject,
					"revoke-before-notice",
					db.AuthorityChange{
						EventID:         f.Event.ID,
						UserID:          f.AmbassadorID,
						Role:            db.AuthorityNone,
						ExpectedVersion: 1,
					},
					uuid.Nil,
				)
				require.NoError(t, err)
			}
			f.H.start(t)
			awaitNotification(t, f, id, "delivery_blocked")
			require.Empty(t, sentNotifications(p, 11003))
		})
	}
}

func TestPaymentNotificationWaitsForIAMAndHonorsTelegramRetryAfter(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f := setupPayment(t, p)
	f.H.stop(t)
	_, id := pendingNotice(t, f, "temporary-outages")
	p.mu.Lock()
	p.iamDown = true
	p.rejectSendOnce = true
	p.mu.Unlock()
	f.H.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 11003)) > 0 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
	p.mu.Lock()
	p.iamDown = false
	p.mu.Unlock()
	retry := awaitNotification(t, f, id, "retryable")
	require.True(t, retry.NextAttemptAt.After(time.Now().Add(time.Second)))
	require.Len(t, sentNotifications(p, 11003), 1)
	awaitNotification(t, f, id, "delivered")
	require.Len(t, sentNotifications(p, 11003), 2)
}

func TestPaymentNotificationSkipsSupersededReviewAlertAndShowsRejection(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f := setupPayment(t, p)
	f.H.stop(t)
	evidence, id := pendingNotice(t, f, "superseded-alert")
	body := preparedPayment(
		t,
		f,
		f.Ambassador,
		"reject-notice",
		payments.ReviewChange{
			EventID:    f.Event.ID,
			EvidenceID: evidence.ID,
			Decision:   payments.Rejected,
			Reason:     "Wrong event proof; please clarify",
		},
	)
	executePayment(t, f, f.Ambassador, body)
	rows := notificationList(t, f)
	require.Len(t, rows, 2)
	var decision uuid.UUID
	for _, row := range rows {
		if row.Kind == payments.Rejected {
			decision = row.ID
		}
	}
	f.H.start(t)
	awaitNotification(t, f, id, "delivery_blocked")
	awaitNotification(t, f, decision, "delivered")
	require.Empty(t, sentNotifications(p, 11003))
	messages := sentNotifications(p, 11002)
	require.Len(t, messages, 1)
	require.Contains(t, messages[0], "credited no money")
	require.Contains(t, messages[0], "Wrong event proof")
}

func TestPaymentNotificationRepairDoesNotStarveHealthyRecipientBehindWaitingPage(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f := setupPayment(t, p)
	f.H.stop(t)
	operator := paymentScope(t, f.H, f.AdminID, true)
	p.mu.Lock()
	p.unavailableTokenSubjects = make(map[string]bool)
	p.mu.Unlock()
	for i := range db.NotificationRepairPageSize {
		id, _ := registrationUser(t, f.H, int64(50000+i), false)
		user, err := f.H.store.GetUser(context.Background(), id)
		require.NoError(t, err)
		p.mu.Lock()
		p.unavailableTokenSubjects[user.Subject] = true
		p.mu.Unlock()
		_, err = f.H.store.ChangeAuthority(
			context.Background(),
			operator.EventScope,
			operator.Subject,
			"waiting-notice-role-"+id.String(),
			db.AuthorityChange{EventID: f.Event.ID, UserID: id, Role: db.AuthorityAmbassador},
			uuid.Nil,
		)
		require.NoError(t, err)
	}
	file := paymentFile(t, f.H, f.OwnerID, 94001, "fair-notification-proof", time.Now().UTC().Truncate(time.Second))
	evidence := submittedPayment(t, f, "fair-notification-submit", file, 3087, payments.Initial)
	// Seed ordering so the healthy recipient is strictly beyond the first bounded page.
	_, err := f.H.store.Pool().Exec(context.Background(), "UPDATE shuttle_payment_notifications SET id=CASE\n "+
		"WHEN recipient_id=$1 THEN 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid\n ELSE "+
		"('00000000-'||substr(md5(id::text),9,4)||'-'||substr(md5(id::text),13,4)||'-'||substr(md5(id::text),17,"+
		"4)||'-'||substr(md5(id::text),21,12))::uuid END\n WHERE evidence_id=$2", f.AmbassadorID, evidence.ID)
	require.NoError(t, err)
	healthy := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	var preceding int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), `SELECT count(*)
 FROM shuttle_payment_notifications WHERE evidence_id=$1 AND id<$2`, evidence.ID, healthy).Scan(&preceding))
	require.Equal(t, db.NotificationRepairPageSize, preceding, "The healthy recipient must follow a full page")
	const queuedSQL = `SELECT EXISTS(SELECT 1 FROM river_job
 WHERE kind='zinusya_payment_notification' AND args->>'notification_id'=$1)`
	var queued bool
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), queuedSQL, healthy.String()).Scan(&queued))
	require.False(t, queued, "Repair must discover the recipient after startup")
	phase := "scheduling beyond the first page"
	t.Cleanup(func() {
		if t.Failed() {
			logPaymentRepairState(t, f, evidence.ID, healthy, phase)
		}
	})
	f.H.start(t)
	// Assert scheduler progress separately from draining failed recipients through
	// the two-worker queue. Poll storage directly to avoid adding API/auth traffic.
	const phaseTimeout = 30 * time.Second
	const pollInterval = 100 * time.Millisecond
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var exists bool
		queryErr := f.H.store.Pool().QueryRow(t.Context(), queuedSQL, healthy.String()).Scan(&exists)
		if assert.NoError(c, queryErr) {
			assert.True(c, exists, "Repair has not queued the healthy recipient")
		}
	}, phaseTimeout, pollInterval, "Repair must advance beyond the full page of unavailable recipients")
	phase = "delivery of the queued healthy recipient"
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var state string
		queryErr := f.H.store.Pool().QueryRow(t.Context(),
			"SELECT state FROM shuttle_payment_notifications WHERE id=$1", healthy).Scan(&state)
		if assert.NoError(c, queryErr) {
			assert.Equal(c, "delivered", state)
		}
	}, phaseTimeout, pollInterval, "The queued healthy recipient must eventually receive the notification")
	require.Len(t, sentNotifications(p, 11003), 1)
}

func logPaymentRepairState(t *testing.T, f paymentFixture, evidence, healthy uuid.UUID, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 2*time.Second)
	defer cancel()
	var snapshot string
	err := f.H.store.Pool().QueryRow(ctx, `SELECT jsonb_build_object(
 'healthy', (SELECT jsonb_build_object('state',state,'version',version,'next_attempt_at',next_attempt_at)
   FROM shuttle_payment_notifications WHERE id=$2),
 'notification_states', (SELECT jsonb_object_agg(state,total) FROM
   (SELECT state,count(*) AS total FROM shuttle_payment_notifications WHERE evidence_id=$1 GROUP BY state) n),
 'queue_states', (SELECT jsonb_object_agg(state,total) FROM
   (SELECT j.state,count(*) AS total FROM river_job j
    JOIN shuttle_payment_notifications n ON n.id::text=j.args->>'notification_id'
    WHERE j.kind='zinusya_payment_notification' AND n.evidence_id=$1 GROUP BY j.state) q),
 'healthy_jobs', (SELECT jsonb_agg(jsonb_build_object('state',state,'attempt',attempt,
   'scheduled_at',scheduled_at,'attempted_at',attempted_at,'finalized_at',finalized_at))
   FROM river_job WHERE kind='zinusya_payment_notification' AND args->>'notification_id'=$2::text)
 )::text`, evidence, healthy).Scan(&snapshot)
	if err != nil {
		t.Logf("Payment repair stalled during %s; diagnostic query failed: %v", phase, err)
		return
	}
	t.Logf("Payment repair stalled during %s: %s", phase, snapshot)
}
