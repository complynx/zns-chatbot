package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

func knowledgeResolutionToken(f knowledgeFixture, actor db.PaymentScope) string {
	p := f.h.peers
	p.mu.Lock()
	defer p.mu.Unlock()
	previous := p.adminRole
	p.adminRole = actor.GlobalAdmin
	token := p.token(actor.Subject)
	p.adminRole = previous
	return token
}

func unknownKnowledgeNotice(t *testing.T) (knowledgeFixture, db.KnowledgeNotification) {
	t.Helper()
	f := newKnowledgeFixture(t)
	knowledgeNoticeSuggestion(t, f)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	rows := knowledgeNotices(t, f)
	require.Len(t, rows, 1)
	f.h.peers.mu.Lock()
	f.h.peers.loseSend = true
	f.h.peers.mu.Unlock()
	startKnowledgeDelivery(t, f)
	n := awaitKnowledgeNotice(t, f, rows[0].ID, "delivery_unknown")
	f.h.stop(t)
	require.Len(t, sentNotifications(f.h.peers, 41002), 1)
	return f, n
}

func TestKnowledgeNoticeResolutionHTTPPreservesBusinessAndOriginalReceipt(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"retry", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f, n := unknownKnowledgeNotice(t)
			token := knowledgeResolutionToken(f, f.admin)
			args := map[string]any{
				"notification_kind": "knowledge",
				"notification_id":   n.ID,
				"expected_version":  n.Version,
				"action":            action,
				"reason":            "Checked original receipt and chat; explicit last-resort choice",
			}
			op := uuid.NewString()
			body := map[string]any{"protocol_version": 1, "operation_id": op, "arguments": args}
			code, raw := adminRequest(t, f.h, token, http.MethodPost, notificationResolutionAPI+"/prepare", body)
			require.Equal(t, http.StatusOK, code, string(raw))
			var prep struct {
				Arguments     map[string]any `json:"arguments"`
				Preconditions map[string]any `json:"preconditions"`
				Description   string         `json:"description"`
			}
			require.NoError(t, json.Unmarshal(raw, &prep))
			require.Contains(t, prep.Description, "no event origin")
			if action == "retry" {
				require.Contains(t, prep.Description, "duplicate")
			}
			body["arguments"] = prep.Arguments
			body["idempotency_key"] = op
			body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions}
			denyKnowledgeResolutionToOrdinaryActors(t, f, body, n.ID)
			code, raw = adminRequest(
				t,
				f.h,
				token,
				http.MethodPost,
				"/internal/agent/administration/knowledge_notification",
				map[string]any{
					"protocol_version": 1,
					"operation_id":     uuid.NewString(),
					"arguments":        map[string]any{"id": n.ID},
				},
			)
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), "attempt_id")
			require.Contains(t, string(raw), "receipt")
			require.NotContains(t, string(raw), "payload")
			require.NotContains(t, string(raw), knowledgeContent().Body)
			code, raw = adminRequest(t, f.h, token, http.MethodPost, notificationResolutionAPI, body)
			require.Equal(t, http.StatusOK, code, string(raw))
			var out struct {
				Status string                          `json:"status"`
				Result db.NotificationResolutionResult `json:"result"`
			}
			require.NoError(t, json.Unmarshal(raw, &out))
			require.Equal(t, "succeeded", out.Status)
			require.JSONEq(t, string(n.Receipt), string(out.Result.PreviousReceipt))
			require.Equal(t, n.Version+1, out.Result.Version)
			f.h.peers.mu.Lock()
			f.h.peers.loseSend = false
			f.h.peers.mu.Unlock()
			f.h.start(t)
			if action == "retry" {
				awaitKnowledgeNotice(t, f, n.ID, "delivered")
				require.Len(t, sentNotifications(f.h.peers, 41002), 2)
			} else {
				require.Equal(t, "dismissed", out.Result.State)
			}
			// Receipt replay stays immutable after expiry and a later delivery generation.
			_, err := f.h.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()-interval "+
					"'1 second' WHERE operation_id=$1", op)
			require.NoError(t, err)
			for _, path := range []string{
				notificationResolutionAPI,
				notificationResolutionAPI + "/status",
				notificationResolutionAPI + "/reconcile",
			} {
				request := body
				if path != notificationResolutionAPI {
					request = map[string]any{"protocol_version": 1, "operation_id": op}
				}
				code, raw = adminRequest(t, f.h, token, http.MethodPost, path, request)
				require.Equal(t, http.StatusOK, code, string(raw))
				var replay struct {
					Result db.NotificationResolutionResult `json:"result"`
				}
				require.NoError(t, json.Unmarshal(raw, &replay))
				require.Equal(t, out.Result, replay.Result)
			}
			f.h.stop(t)
			f.h.start(t)
			want := 1
			if action == "retry" {
				want = 2
			}
			require.Never(
				t,
				func() bool { return len(sentNotifications(f.h.peers, 41002)) > want },
				1200*time.Millisecond,
				25*time.Millisecond,
			)
			var suggestions, operations, articles int
			require.NoError(
				t,
				f.h.store.Pool().
					QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_knowledge_suggestions WHERE state='pending'),"+
						"(SELECT count(*) FROM shuttle_knowledge_operations),(SELECT count(*) FROM "+
						"shuttle_knowledge_articles)").
					Scan(&suggestions, &operations, &articles),
			)
			require.Equal(t, 1, suggestions)
			require.Equal(t, 2, operations)
			require.Zero(t, articles)
		})
	}
}

func TestKnowledgeNoticeResolutionRejectsChangedExpiredAndRevokedTargets(t *testing.T) {
	t.Parallel()
	f, n := unknownKnowledgeNotice(t)
	args := db.NotificationResolution{
		NotificationKind: "knowledge",
		NotificationID:   n.ID,
		ExpectedVersion:  n.Version,
		Action:           "retry",
		Reason:           "Checked receipt",
	}
	op := uuid.NewString()
	prep, err := f.h.store.PrepareNotificationResolution(t.Context(), f.admin, op, args)
	require.NoError(t, err)
	changed := args
	changed.Action = "dismiss"
	_, err = f.h.store.ResolveNotification(t.Context(), f.admin, op, changed, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.h.store.ResolveNotification(t.Context(), f.admin, op, args, uuid.New())
	require.ErrorIs(t, err, db.ErrConflict)
	// A different event must not rebind a general/history notice.
	changed = args
	changed.EventID = uuid.New()
	_, err = f.h.store.PrepareNotificationResolution(t.Context(), f.admin, uuid.NewString(), changed)
	require.Error(t, err)
	_, err = f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()-interval "+
			"'1 second' WHERE operation_id=$1", op)
	require.NoError(t, err)
	_, err = f.h.store.ResolveNotification(t.Context(), f.admin, op, args, prep.ID)
	require.ErrorIs(t, err, db.ErrConflict)
	stale := args
	stale.ExpectedVersion--
	_, err = f.h.store.PrepareNotificationResolution(t.Context(), f.admin, uuid.NewString(), stale)
	require.ErrorIs(t, err, db.ErrConflict)
	enabled := false
	knowledgeApply(
		t,
		f,
		f.admin,
		db.KnowledgeChange{
			Kind:                     "set_moderator",
			ModeratorUserID:          f.moderator.UserID,
			ModeratorEnabled:         &enabled,
			ExpectedModeratorVersion: 1,
			Reason:                   "Revoke recipient before manual retry",
		},
	)
	op = uuid.NewString()
	prep, err = f.h.store.PrepareNotificationResolution(t.Context(), f.admin, op, args)
	require.NoError(t, err)
	_, err = f.h.store.ResolveNotification(t.Context(), f.admin, op, args, prep.ID)
	require.Error(t, err)
	args.Action = "dismiss"
	op = uuid.NewString()
	prep, err = f.h.store.PrepareNotificationResolution(t.Context(), f.admin, op, args)
	require.NoError(t, err)
	result, err := f.h.store.ResolveNotification(t.Context(), f.admin, op, args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, "dismissed", result.State)
	require.Len(t, sentNotifications(f.h.peers, 41002), 1)
}

func TestKnowledgeNoticeResolutionRequiresOriginalEventEvenAfterEnd(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	event, err := f.h.store.ChangeEvent(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.EventChange{
			ID: uuid.New(),
			Document: events.Document{
				Name:     "Notice origin",
				Timezone: "Europe/Minsk",
				StartsAt: now.Add(-48 * time.Hour),
				EndsAt:   now.Add(time.Hour),
				Modules:  []string{},
			},
		},
	)
	require.NoError(t, err)
	_, err = f.h.store.ChangeAuthority(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.AuthorityChange{EventID: event.ID, UserID: f.eventAdmin.UserID, Role: db.AuthorityAdministrator},
		uuid.Nil,
	)
	require.NoError(t, err)
	content := knowledgeContent()
	suggestion := knowledgeApply(
		t,
		f,
		f.user,
		db.KnowledgeChange{
			Kind:      "suggest",
			ArticleID: uuid.New(),
			EventID:   &event.ID,
			Content:   &content,
			Reason:    "Synthetic event proposal",
		},
	)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	rows := knowledgeNotices(t, f)
	require.Len(t, rows, 1)
	n, err := f.h.store.ReserveKnowledgeNotification(
		t.Context(),
		f.eventAdmin,
		rows[0].ID,
		rows[0].Version,
		json.RawMessage(`{}`),
	)
	require.NoError(t, err)
	require.NoError(
		t,
		f.h.store.FinishKnowledgeNotification(t.Context(), n, "delivery_unknown", json.RawMessage(`{}`), 0),
	)
	n, err = f.h.store.KnowledgeNotification(t.Context(), f.admin.EventScope, n.ID)
	require.NoError(t, err)
	require.Equal(t, suggestion.Suggestion.ID, n.SuggestionID)
	ended := event.Document
	ended.EndsAt = now.Add(-time.Second)
	_, err = f.h.store.ChangeEvent(
		t.Context(),
		f.admin.EventScope,
		f.admin.Subject,
		uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: ended},
	)
	require.NoError(t, err)
	args := db.NotificationResolution{
		NotificationKind: "knowledge",
		NotificationID:   n.ID,
		ExpectedVersion:  n.Version,
		Action:           "dismiss",
		Reason:           "Close historical delivery without claiming receipt",
	}
	_, err = f.h.store.PrepareNotificationResolution(t.Context(), f.admin, uuid.NewString(), args)
	require.Error(t, err)
	args.EventID = event.ID
	op := uuid.NewString()
	prep, err := f.h.store.PrepareNotificationResolution(t.Context(), f.admin, op, args)
	require.NoError(t, err)
	result, err := f.h.store.ResolveNotification(t.Context(), f.admin, op, args, prep.ID)
	require.NoError(t, err)
	require.Equal(t, "dismissed", result.State)
	current, err := f.h.store.ReadKnowledgeSuggestion(t.Context(), f.user, n.SuggestionID)
	require.NoError(t, err)
	require.Equal(t, "pending", current.State)
}

func denyKnowledgeResolutionToOrdinaryActors(t *testing.T, f knowledgeFixture, body map[string]any, id uuid.UUID) {
	t.Helper()
	for _, actor := range []db.PaymentScope{f.user, f.moderator} {
		denied := knowledgeResolutionToken(f, actor)
		for _, path := range []string{
			notificationResolutionAPI + "/prepare",
			notificationResolutionAPI,
			notificationResolutionAPI + "/status",
			"/internal/agent/administration/knowledge_notification",
		} {
			request := body
			switch path {
			case notificationResolutionAPI + "/prepare":
				request = map[string]any{
					"protocol_version": 1,
					"operation_id":     body["operation_id"],
					"arguments":        body["arguments"],
				}
			case notificationResolutionAPI + "/status":
				request = map[string]any{"protocol_version": 1, "operation_id": body["operation_id"]}
			}
			if path == "/internal/agent/administration/knowledge_notification" {
				request = map[string]any{
					"protocol_version": 1,
					"operation_id":     uuid.NewString(),
					"arguments":        map[string]any{"id": id},
				}
			}
			code, _ := adminRequest(t, f.h, denied, http.MethodPost, path, request)
			require.Equal(t, http.StatusForbidden, code, path)
		}
	}
}
