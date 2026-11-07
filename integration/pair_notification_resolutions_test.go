package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestPairNotificationAdminResolutionPreservesConsentAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"retry", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := setupPair(t)
			f.H.stop(t)
			id := pairPendingNotice(t, f)
			f.H.peers.mu.Lock()
			f.H.peers.loseSend = true
			ordinary := f.H.peers.token(f.A.Subject)
			f.H.peers.mu.Unlock()
			adminID, admin := registrationUser(t, f.H, 12005, true)
			f.H.start(t)
			awaitPairNotice(t, f, id, "delivery_unknown")
			code, data := adminRequest(
				t,
				f.H,
				admin,
				http.MethodPost,
				"/internal/agent/administration/pair_notifications",
				map[string]any{
					"protocol_version": 1,
					"operation_id":     "list-pair-notices",
					"arguments":        map[string]any{"state": "delivery_unknown"},
				},
			)
			require.Equal(t, http.StatusOK, code, string(data))
			require.Contains(t, string(data), id.String())
			code, data = adminRequest(
				t,
				f.H,
				admin,
				http.MethodPost,
				"/internal/agent/administration/pair_notification",
				map[string]any{
					"protocol_version": 1,
					"operation_id":     "inspect-pair-notice",
					"arguments":        map[string]any{"id": id},
				},
			)
			require.Equal(t, http.StatusOK, code, string(data))
			var detail struct {
				Result db.PairInvitationDelivery `json:"result"`
			}
			require.NoError(t, json.Unmarshal(data, &detail))
			require.Equal(t, id, detail.Result.ID)
			require.Equal(t, "delivery_unknown", detail.Result.State)
			require.Equal(t, f.EventID, detail.Result.Context.Invitation.EventID)
			require.NotContains(t, string(data), "payload")
			args := db.NotificationResolution{
				NotificationKind: "pair",
				EventID:          f.EventID,
				NotificationID:   id,
				ExpectedVersion:  detail.Result.Version,
				Action:           action,
				Reason:           "Checked actual test chat and receipt; explicit last resort decision",
			}
			body := map[string]any{"protocol_version": 1, "operation_id": "pair-resolution", "arguments": args}
			code, data = adminRequest(t, f.H, ordinary, http.MethodPost, notificationResolutionAPI+"/prepare", body)
			require.Equal(t, http.StatusForbidden, code, string(data))
			code, data = adminRequest(t, f.H, admin, http.MethodPost, notificationResolutionAPI+"/prepare", body)
			require.Equal(t, http.StatusOK, code, string(data))
			var prep struct {
				Preconditions map[string]any `json:"preconditions"`
				Description   string         `json:"description"`
			}
			require.NoError(t, json.Unmarshal(data, &prep))
			require.Contains(t, prep.Description, "Pair invitation")
			body["idempotency_key"] = "pair-resolution"
			body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
			wrongKind := args
			wrongKind.NotificationKind = "payment"
			body["arguments"] = wrongKind
			code, data = adminRequest(t, f.H, admin, http.MethodPost, notificationResolutionAPI, body)
			require.Equal(t, http.StatusOK, code, string(data))
			require.Contains(t, string(data), `"status":"rejected"`)
			body["arguments"] = args
			first := executePairNoticeResolution(t, f, admin, body)
			require.Equal(t, "pair", first.NotificationKind)
			require.Equal(t, detail.Result.Receipt, first.PreviousReceipt)
			require.Equal(t, detail.Result.Version+1, first.Version)
			state := "dismissed"
			if action == "retry" {
				state = "delivered"
			}
			awaitPairNotice(t, f, id, state)
			f.H.stop(t)
			_, err := f.H.store.Pool().
				Exec(ctx, "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()-interval '1 second' "+
					"WHERE operation_id='pair-resolution'")
			require.NoError(t, err)
			f.H.start(t)
			require.Equal(t, first, executePairNoticeResolution(t, f, admin, body))
			for _, suffix := range []string{"status", "reconcile"} {
				code, data = adminRequest(t, f.H, admin, http.MethodPost, notificationResolutionAPI+"/"+suffix,
					map[string]any{"protocol_version": 1, "operation_id": "pair-resolution"})
				require.Equal(t, http.StatusOK, code, string(data))
				var receipt struct {
					Result db.NotificationResolutionResult `json:"result"`
				}
				require.NoError(t, json.Unmarshal(data, &receipt))
				require.Equal(t, first, receipt.Result)
			}
			invitation, err := f.H.store.PairInvitation(ctx, f.A, f.EventID, detail.Result.InvitationID)
			require.NoError(t, err)
			require.Equal(t, "pending", invitation.State)
			require.Equal(t, int64(1), invitation.Version)
			wantCount := 1
			if action == "retry" {
				wantCount = 2
			}
			require.Len(t, sentNotifications(f.H.peers, 12003), wantCount)
			require.NoError(t, f.H.store.RevokeUser(ctx, adminID, true))
			code, data = adminRequest(t, f.H, admin, http.MethodPost, notificationResolutionAPI+"/status",
				map[string]any{"protocol_version": 1, "operation_id": "pair-resolution"})
			require.Equal(t, http.StatusForbidden, code, string(data))
		})
	}
}

func executePairNoticeResolution(
	t *testing.T,
	f pairFixture,
	admin string,
	body map[string]any,
) db.NotificationResolutionResult {
	t.Helper()
	code, data := adminRequest(t, f.H, admin, http.MethodPost, notificationResolutionAPI, body)
	require.Equal(t, http.StatusOK, code, string(data))
	var out struct {
		Status string                          `json:"status"`
		Result db.NotificationResolutionResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &out))
	require.Equal(t, "succeeded", out.Status, string(data))
	return out.Result
}
