package integration_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func orderNoticeList(t *testing.T, f paymentFixture) []db.NotificationSummary {
	t.Helper()
	code, raw := agentAdminRead(t, f.H, f.Admin, "order_notifications", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var body struct {
		Result struct {
			Notifications []db.NotificationSummary `json:"notifications"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	return body.Result.Notifications
}

func awaitOrderNotice(t *testing.T, f paymentFixture, id uuid.UUID, state string) db.OrderNotification {
	t.Helper()
	var n db.OrderNotification
	scope := db.EventScope{BotID: 123, Organization: "org"}
	require.Eventually(t, func() bool {
		var err error
		n, err = f.H.store.OrderNotification(t.Context(), scope, id)
		return err == nil && n.State == state
	},
		10*time.Second, 25*time.Millisecond)
	return n
}

func onlyOwnerOrderNotice(t *testing.T, f paymentFixture) {
	t.Helper()
	_, err := f.H.store.Pool().
		Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
}

func TestOrderNotificationDeliveryResumesAndKeepsOneNoticePerRecipient(t *testing.T) {
	t.Parallel()
	f, scope, order := orderEvidenceFixture(t)
	f.H.stop(t)
	p := f.H.peers
	p.miniAppLaunchURL = "https://fixture.invalid/app/"
	// Assign authority before admission: the producer sees this owner twice.
	_, err := f.H.store.Pool().
		Exec(t.Context(), "INSERT INTO shuttle_event_authorities(event_id,user_id,role,version) VAL" +
"UES($1,$2,'ambassador',1)", f.Event.ID, scope.UserID)
	require.NoError(t, err)
	operation := uuid.New()
	args := orderEvidenceArgs(t, f, order, 99830, "dual-role-notice-proof")
	admitted, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, operation, args)
	require.NoError(t, err)
	n := db.OrderNotification{Evidence: admitted, Order: order, EvidenceID: admitted.ID}
	replay, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, operation, args)
	require.NoError(t, err)
	require.Equal(t, admitted, replay)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_users SET profile=$2 WHERE id=$1", scope.UserID, json.RawMessage(`{"language_code":"uk"}`))
	require.NoError(t, err)
	rows := orderNoticeList(t, f)
	require.Len(t, rows, 2)
	f.H.start(t)
	for _, row := range rows {
		awaitOrderNotice(t, f, row.ID, "delivered")
	}
	owner := sentNotifications(p, 11002)
	ambassador := sentNotifications(p, 11003)
	require.Len(t, owner, 1)
	require.Len(t, ambassador, 1)
	require.Contains(t, owner[0], "Оплата ожидает проверки человеком")
	require.Contains(t, ambassador[0], "Payment awaits human review")
	require.Contains(t, ambassador[0], n.Order.Snapshot.Quote.Timezone)
	p.mu.Lock()
	messages := append([]telegram.SendMessageRequest(nil), p.sent...)
	runCount := len(p.createKeys)
	p.mu.Unlock()
	for _, message := range messages {
		keyboard, ok := message.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
		require.True(t, ok)
		require.Equal(
			t,
			"https://fixture.invalid/app/#order-review?event="+f.Event.ID.String()+"&evidence="+n.EvidenceID.String(),
			keyboard.InlineKeyboard[0][0].WebApp.URL,
		)
	}
	require.Zero(t, runCount)
	f.H.stop(t)
	f.H.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 11002)) > 1 || len(sentNotifications(p, 11003)) > 1 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
}

func TestOrderNotificationDeliveryHumanDecisionsSupersedePendingMessages(t *testing.T) {
	t.Parallel()
	for _, decision := range []string{db.OrderEvidenceConfirmed, db.OrderEvidenceRejected} {
		t.Run(decision, func(t *testing.T) {
			t.Parallel()
			f, _, n := pendingOrderNotice(t)
			_, _, reviewed := reviewOrderEvidence(t, f, n.Evidence, decision)
			rows := orderNoticeList(t, f)
			require.Len(t, rows, 4)
			f.H.start(t)
			for _, row := range rows {
				want := "delivery_blocked"
				if row.Kind == decision {
					want = "delivered"
				}
				result := awaitOrderNotice(t, f, row.ID, want)
				if row.Kind == decision {
					require.Equal(t, reviewed, result.Evidence)
				}
			}
			for _, chat := range []int64{11002, 11003} {
				messages := sentNotifications(f.H.peers, chat)
				require.Len(t, messages, 1)
				if decision == db.OrderEvidenceConfirmed {
					require.Contains(t, messages[0], "confirmed by the reviewer")
				} else {
					require.Contains(t, messages[0], "not cancelled")
					require.Contains(t, messages[0], "no refund")
				}
			}
			require.Zero(t, orderAllocationUnits(t, f, "held"))
		})
	}
}

func TestOrderNotificationDeliveryUnknownRequiresAuthorizedExactResolution(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"retry", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f, _, n := pendingOrderNotice(t)
			onlyOwnerOrderNotice(t, f)
			p := f.H.peers
			p.mu.Lock()
			p.loseSend = true
			p.mu.Unlock()
			f.H.start(t)
			unknown := awaitOrderNotice(t, f, n.ID, "delivery_unknown")
			f.H.stop(t)
			require.Len(t, sentNotifications(p, 11002), 1)
			f.H.start(t)
			awaitOrderNotice(t, f, n.ID, "delivery_unknown")
			require.Never(
				t,
				func() bool { return len(sentNotifications(p, 11002)) > 1 },
				1200*time.Millisecond,
				25*time.Millisecond,
			)
			f.H.stop(t)
			args := db.NotificationResolution{
				NotificationKind: "order",
				EventID:          f.Event.ID,
				NotificationID:   n.ID,
				ExpectedVersion:  unknown.Version,
				Action:           action,
				Reason:           "Checked synthetic chat and receipt; last resort",
			}
			body := preparedNotification(t, f, "order-notice-resolution", args)
			for _, token := range []string{f.Owner, f.Ambassador} {
				code, _ := adminRequest(t, f.H, token, http.MethodPost, notificationResolutionAPI, body)
				require.Equal(t, http.StatusForbidden, code)
				code, _ = agentAdminRead(t, f.H, token, "order_notification", map[string]any{"id": n.ID})
				require.Equal(t, http.StatusForbidden, code)
			}
			result := resolvedNotification(t, f, body)
			require.Equal(t, "order", result.NotificationKind)
			require.JSONEq(t, string(unknown.Receipt), string(result.PreviousReceipt))
			_, err := f.H.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()" +
"-interval '1 second' WHERE operation_id=$1", "order-notice-resolution")
			require.NoError(t, err)
			f.H.start(t)
			if action == "retry" {
				awaitOrderNotice(t, f, n.ID, "delivered")
				require.Len(t, sentNotifications(p, 11002), 2)
			} else {
				awaitOrderNotice(t, f, n.ID, "dismissed")
				require.Len(t, sentNotifications(p, 11002), 1)
			}
			require.Equal(t, result, resolvedNotification(t, f, body))
			code, raw := adminRequest(
				t,
				f.H,
				f.Admin,
				http.MethodPost,
				notificationResolutionAPI+"/status",
				map[string]any{"protocol_version": 1, "operation_id": "order-notice-resolution"},
			)
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), `"notification_kind":"order"`)
			code, raw = agentAdminRead(t, f.H, f.Admin, "order_notification", map[string]any{"id": n.ID})
			require.Equal(t, http.StatusOK, code, string(raw))
			require.NotContains(t, string(raw), `"payload"`)
			var reviewCount int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), "SELECT count(*) FROM shuttle_order_evidence_reviews").
					Scan(&reviewCount),
			)
			require.Zero(t, reviewCount)
			require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
		})
	}
}

func TestOrderNotificationDeliveryRechecksAuthorityAfterIAMWait(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"role", "module"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, _, n := pendingOrderNotice(t)
			p := f.H.peers
			amb := paymentScope(t, f.H, f.AmbassadorID, false)
			entered, release := make(chan struct{}), make(chan struct{})
			var signal, unblock sync.Once
			t.Cleanup(func() { unblock.Do(func() { close(release) }) })
			p.tokenHook = func(subject string) {
				if subject == amb.Subject {
					signal.Do(func() { close(entered); <-release })
				}
			}
			var ambassadorNotice uuid.UUID
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), "SELECT id FROM shuttle_order_evidence_notices WHERE evidence_id=$1 AND r" +
"ecipient_id=$2", n.EvidenceID, f.AmbassadorID).
					Scan(&ambassadorNotice),
			)
			f.H.start(t)
			select {
			case <-entered:
			case <-time.After(8 * time.Second):
				t.Fatal("notification did not reach IAM wait")
			}
			var err error
			if change == "role" {
				_, err = f.H.store.Pool().
					Exec(t.Context(), "DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2", f.Event.ID, f.AmbassadorID)
			} else {
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'orders') WHERE id=$1", f.Event.ID)
			}
			require.NoError(t, err)
			unblock.Do(func() { close(release) })
			awaitOrderNotice(t, f, ambassadorNotice, "delivery_blocked")
			require.Empty(t, sentNotifications(p, 11003))
		})
	}
}

func TestOrderNotificationDeliveryWaitsForIAMAndRetriesOnlyKnownTelegramRejection(t *testing.T) {
	t.Parallel()
	f, owner, n := pendingOrderNotice(t)
	onlyOwnerOrderNotice(t, f)
	p := f.H.peers
	entered := make(chan struct{}, 1)
	p.unavailableTokenSubjects = map[string]bool{owner.Subject: true}
	p.tokenHook = func(subject string) {
		if subject == owner.Subject {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
	}
	f.H.start(t)
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("notification did not attempt IAM")
	}
	pending := awaitOrderNotice(t, f, n.ID, "pending")
	require.Equal(t, uuid.Nil, pending.AttemptID)
	f.H.stop(t)
	p.mu.Lock()
	p.unavailableTokenSubjects = nil
	p.loomDown = true
	p.rejectSendOnce = true
	p.mu.Unlock()
	f.H.start(t)
	awaitOrderNotice(t, f, n.ID, "retryable")
	awaitOrderNotice(t, f, n.ID, "delivered")
	require.Len(t, sentNotifications(p, 11002), 2)
	p.mu.Lock()
	require.Empty(t, p.createKeys)
	p.mu.Unlock()
	require.Equal(t, int64(8), orderAllocationUnits(t, f, "held"))
}

func TestOrderNotificationDeliveryAdmissionExceptionsPreserveHumanDecision(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{db.OrderEvidenceExhausted, db.OrderEvidenceNeedsDecision} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, scope, args, _, _, _ := orderCapacityFixture(t)
			f.H.stop(t)
			order := additionalEvidenceOrder(t, f, scope, args.ExpectedDraftVersion)
			proof := orderEvidenceArgs(t, f, order, 99820, "exception-proof")
			if kind == db.OrderEvidenceNeedsDecision {
				proof.AmountMinor--
			}
			evidence, err := f.H.store.SubmitOwnOrderEvidence(t.Context(), scope, uuid.New(), proof)
			require.NoError(t, err)
			require.Equal(t, kind, evidence.State)
			rows := orderNoticeList(t, f)
			require.Len(t, rows, 2)
			f.H.start(t)
			for _, row := range rows {
				awaitOrderNotice(t, f, row.ID, "delivered")
			}
			owner := sentNotifications(f.H.peers, 11002)
			amb := sentNotifications(f.H.peers, 11003)
			require.Len(t, owner, 1)
			require.Len(t, amb, 1)
			if kind == db.OrderEvidenceExhausted {
				require.Contains(t, owner[0], "Places have run out")
				require.Contains(t, amb[0], "refund requires attention")
			} else {
				require.Contains(t, owner[0], "human case decision")
				require.Contains(t, amb[0], "Payment is not confirmed")
			}
			require.Zero(t, orderAllocationUnits(t, f, "held"))
			require.Zero(t, orderAllocationUnits(t, f, "occupied"))
		})
	}
}
