package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const notificationResolutionAPI = "/internal/agent/administration/resolve_notification"

func preparedNotification(t *testing.T, f paymentFixture, op string, args db.NotificationResolution) map[string]any {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args}
	code, data := adminRequest(t, f.H, f.Admin, http.MethodPost, notificationResolutionAPI+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(data))
	var prep struct {
		Arguments     db.NotificationResolution `json:"arguments"`
		Description   string                    `json:"description"`
		Preconditions map[string]any            `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(data, &prep))
	require.Equal(t, args, prep.Arguments)
	if args.Action == "retry" {
		require.Contains(t, prep.Description, "duplicate")
	}
	body["idempotency_key"] = op
	body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
	return body
}

func resolvedNotification(t *testing.T, f paymentFixture, body map[string]any) db.NotificationResolutionResult {
	t.Helper()
	code, data := adminRequest(t, f.H, f.Admin, http.MethodPost, notificationResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(data))
	var out struct {
		Status string                          `json:"status"`
		Result db.NotificationResolutionResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "succeeded", out.Status, string(data))
	return out.Result
}

func unknownNotification(t *testing.T, p *peers) (paymentFixture, db.PaymentNotification) {
	t.Helper()
	f := setupPayment(t, p)
	f.H.stop(t)
	_, id := pendingNotice(t, f, "resolution-submission")
	p.mu.Lock()
	p.loseSend = true
	p.mu.Unlock()
	f.H.start(t)
	n := awaitNotification(t, f, id, "delivery_unknown")
	f.H.stop(t)
	require.Len(t, sentNotifications(p, 11003), 1)
	return f, n
}

func TestNotificationResolutionRetryAndDismissPreserveMoneyAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"retry", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			f, n := unknownNotification(t, p)
			args := db.NotificationResolution{
				EventID:         f.Event.ID,
				NotificationID:  n.ID,
				ExpectedVersion: n.Version,
				Action:          action,
				Reason:          "Checked receipt and test chat; last resort resolution",
			}
			body := preparedNotification(t, f, "resolution-op", args)
			for _, token := range []string{f.Owner, f.Ambassador} {
				code, _ := adminRequest(t, f.H, token, http.MethodPost, notificationResolutionAPI, body)
				require.Equal(t, http.StatusForbidden, code)
			}
			result := resolvedNotification(t, f, body)
			require.Equal(t, n.Receipt, result.PreviousReceipt)
			require.Equal(t, "delivery_unknown", result.PreviousState)
			require.Equal(t, n.Version+1, result.Version)
			wantState := "dismissed"
			if action == "retry" {
				wantState = "pending"
			}
			require.Equal(t, wantState, result.State)
			// An expired proposal cannot invalidate a previously committed operation receipt.
			_, err := f.H.store.Pool().
				Exec(context.Background(), "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()-interval '1 second' WHERE operation_id='resolution-op'")
			require.NoError(t, err)
			p.mu.Lock()
			p.loseSend = false
			p.mu.Unlock()
			f.H.start(t)
			if action == "retry" {
				awaitNotification(t, f, n.ID, "delivered")
				require.Len(t, sentNotifications(p, 11003), 2)
			} else {
				require.Equal(t, "dismissed", notificationDetail(t, f, n.ID).State)
				require.Never(
					t,
					func() bool { return len(sentNotifications(p, 11003)) > 1 },
					1200*time.Millisecond,
					25*time.Millisecond,
				)
			}
			require.Equal(t, result, resolvedNotification(t, f, body))
			for _, suffix := range []string{"status", "reconcile"} {
				code, data := adminRequest(
					t,
					f.H,
					f.Admin,
					http.MethodPost,
					notificationResolutionAPI+"/"+suffix,
					map[string]any{"protocol_version": 1, "operation_id": "resolution-op"},
				)
				require.Equal(t, http.StatusOK, code, string(data))
				var status struct {
					Result db.NotificationResolutionResult `json:"result"`
				}
				require.NoError(t, json.Unmarshal(data, &status))
				require.Equal(t, result, status.Result)
			}
			var reviews int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(context.Background(), "SELECT count(*) FROM shuttle_payment_reviews").
					Scan(&reviews),
			)
			require.Zero(t, reviews)
			code, data := paymentRequest(
				t,
				f,
				f.Owner,
				"balance",
				"resolution-balance",
				map[string]any{"event_id": f.Event.ID, "pass_id": f.Pass.ID},
			)
			require.Equal(t, http.StatusOK, code, string(data))
			require.Contains(t, string(data), `"confirmed_minor":0`)
			f.H.stop(t)
			f.H.start(t)
			expected := 1
			if action == "retry" {
				expected = 2
			}
			require.Never(
				t,
				func() bool { return len(sentNotifications(p, 11003)) > expected },
				1200*time.Millisecond,
				25*time.Millisecond,
			)
		})
	}
}

func TestNotificationResolutionRejectsExpiredChangedAndForeignProposals(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	f, n := unknownNotification(t, p)
	args := db.NotificationResolution{
		EventID:         f.Event.ID,
		NotificationID:  n.ID,
		ExpectedVersion: n.Version,
		Action:          "retry",
		Reason:          "Verified uncertain delivery",
	}
	body := preparedNotification(t, f, "expired-resolution", args)
	changed := args
	changed.Action = "dismiss"
	body["arguments"] = changed
	code, data := adminRequest(t, f.H, f.Admin, http.MethodPost, notificationResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	body["arguments"] = args
	_, err := f.H.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()-interval '1 second' WHERE operation_id='expired-resolution'")
	require.NoError(t, err)
	code, data = adminRequest(t, f.H, f.Admin, http.MethodPost, notificationResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"status":"rejected"`)
	require.Equal(t, n.Version, notificationDetail(t, f, n.ID).Version)
	args.EventID = uuid.New()
	code, _ = adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		notificationResolutionAPI+"/prepare",
		map[string]any{"protocol_version": 1, "operation_id": "foreign-resolution", "arguments": args},
	)
	require.Equal(t, http.StatusNotFound, code)
	require.Len(t, sentNotifications(p, 11003), 1)
}
