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
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func knowledgeNoticeSuggestion(t *testing.T, f knowledgeFixture) db.KnowledgeSuggestion {
	t.Helper()
	content := knowledgeContent()
	result := knowledgeApply(
		t,
		f,
		f.user,
		db.KnowledgeChange{
			Kind:      "suggest",
			ArticleID: uuid.New(),
			Content:   &content,
			Reason:    "Synthetic suggestion for delivery acceptance",
		},
	)
	require.NotNil(t, result.Suggestion)
	return *result.Suggestion
}

func knowledgeNotices(t *testing.T, f knowledgeFixture) []db.KnowledgeNotification {
	t.Helper()
	page, err := f.h.store.ListKnowledgeNotifications(t.Context(), f.admin, uuid.Nil, 20)
	require.NoError(t, err)
	return page.Items
}

func awaitKnowledgeNotice(t *testing.T, f knowledgeFixture, id uuid.UUID, state string) db.KnowledgeNotification {
	t.Helper()
	var n db.KnowledgeNotification
	require.Eventually(t, func() bool {
		var err error
		n, err = f.h.store.KnowledgeNotification(t.Context(), f.admin.EventScope, id)
		return err == nil && n.State == state
	}, 10*time.Second, 25*time.Millisecond)
	return n
}

func startKnowledgeDelivery(t *testing.T, f knowledgeFixture) {
	t.Helper()
	f.h.start(t)
	t.Cleanup(func() { f.h.stop(t) })
}

func TestKnowledgeNoticesDeliverReviewAndDecisionOnceAcrossRestart(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	f.h.peers.miniAppLaunchURL = "https://fixture.invalid/app/"
	suggestion := knowledgeNoticeSuggestion(t, f)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	rows := knowledgeNotices(t, f)
	require.Len(t, rows, 1)
	require.Equal(t, f.moderator.UserID, rows[0].RecipientID)
	startKnowledgeDelivery(t, f)
	awaitKnowledgeNotice(t, f, rows[0].ID, "delivered")
	require.Len(t, sentNotifications(f.h.peers, 41002), 1)
	f.h.stop(t)
	_, err := f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_users SET profile=$2 WHERE id=$1", f.user.UserID, json.RawMessage(`{"language_code":"uk"}`))
	require.NoError(t, err)
	knowledgeApply(
		t,
		f,
		f.moderator,
		db.KnowledgeChange{
			Kind:         "review",
			SuggestionID: suggestion.ID,
			Decision:     "approve",
			Reason:       "Synthetic independent approval",
		},
	)
	f.h.start(t)
	require.Eventually(
		t,
		func() bool { rows = knowledgeNotices(t, f); return len(rows) == 2 },
		10*time.Second,
		25*time.Millisecond,
	)
	for _, n := range rows {
		awaitKnowledgeNotice(t, f, n.ID, "delivered")
	}
	require.Len(t, sentNotifications(f.h.peers, 41002), 1)
	owner := sentNotifications(f.h.peers, 41003)
	require.Len(t, owner, 1)
	require.Contains(t, owner[0], "одобрил")
	p := f.h.peers
	p.mu.Lock()
	messages := append([]telegram.SendMessageRequest(nil), p.sent...)
	runs := len(p.createKeys)
	p.mu.Unlock()
	require.Zero(t, runs)
	for _, message := range messages {
		require.NotContains(t, message.Text, suggestion.Content.Title)
		require.NotContains(t, message.Text, suggestion.Content.Body)
		keyboard, ok := message.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
		require.True(t, ok)
		require.Equal(
			t,
			"https://fixture.invalid/app/#knowledge?suggestion="+suggestion.ID.String(),
			keyboard.InlineKeyboard[0][0].WebApp.URL,
		)
	}
	f.h.stop(t)
	f.h.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 41002)) > 1 || len(sentNotifications(p, 41003)) > 1 },
		250*time.Millisecond,
		25*time.Millisecond,
	)
}

func TestKnowledgeNoticesBlockRevokedAndSupersededRecipients(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{"moderator revoked", "account revoked", "suggestion decided"} {
		t.Run(reason, func(t *testing.T) {
			t.Parallel()
			f := newKnowledgeFixture(t)
			suggestion := knowledgeNoticeSuggestion(t, f)
			require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
			rows := knowledgeNotices(t, f)
			require.Len(t, rows, 1)
			switch reason {
			case "moderator revoked":
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
						Reason:                   "Synthetic access revocation",
					},
				)
			case "account revoked":
				_, err := f.h.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", f.moderator.UserID)
				require.NoError(t, err)
			case "suggestion decided":
				knowledgeApply(
					t,
					f,
					f.moderator,
					db.KnowledgeChange{
						Kind:         "review",
						SuggestionID: suggestion.ID,
						Decision:     "reject",
						Reason:       "Synthetic decline",
					},
				)
			}
			_, err := f.h.store.ReserveKnowledgeNotification(
				t.Context(),
				f.moderator,
				rows[0].ID,
				rows[0].Version,
				json.RawMessage(`{}`),
			)
			require.Error(t, err)
			startKnowledgeDelivery(t, f)
			awaitKnowledgeNotice(t, f, rows[0].ID, "delivery_blocked")
			require.Empty(t, sentNotifications(f.h.peers, 41002))
			if reason == "suggestion decided" {
				require.Eventually(
					t,
					func() bool { return len(sentNotifications(f.h.peers, 41003)) == 1 },
					10*time.Second,
					25*time.Millisecond,
				)
				require.Contains(t, sentNotifications(f.h.peers, 41003)[0], "declined")
			}
		})
	}
}

func TestKnowledgeNoticesHandPendingEventToHistoricalModerators(t *testing.T) {
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
				Name:     "Notice lifecycle",
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
	article := knowledgeApply(
		t,
		f,
		f.eventAdmin,
		db.KnowledgeChange{
			Kind:      "edit",
			ArticleID: uuid.New(),
			EventID:   &event.ID,
			Content:   &content,
			Reason:    "Publish event source",
		},
	)
	content.Body += "\nProposed event correction."
	knowledgeApply(
		t,
		f,
		f.user,
		db.KnowledgeChange{
			Kind:             "suggest",
			ArticleID:        article.Article.ID,
			EventID:          &event.ID,
			ExpectedRevision: 1,
			Content:          &content,
			Reason:           "A pending event correction",
		},
	)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	rows := knowledgeNotices(t, f)
	require.Len(t, rows, 1)
	require.Equal(t, f.eventAdmin.UserID, rows[0].RecipientID)
	old := rows[0]
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
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	rows = knowledgeNotices(t, f)
	require.Len(t, rows, 2)
	var history db.KnowledgeNotification
	for _, n := range rows {
		if n.RecipientID == f.moderator.UserID {
			history = n
		}
	}
	require.NotEqual(t, uuid.Nil, history.ID)
	startKnowledgeDelivery(t, f)
	awaitKnowledgeNotice(t, f, old.ID, "delivery_blocked")
	awaitKnowledgeNotice(t, f, history.ID, "delivered")
	require.Empty(t, sentNotifications(f.h.peers, 41005))
	require.Len(t, sentNotifications(f.h.peers, 41002), 1)
}

func TestKnowledgeNoticesAmbiguousSendAndReservationSurviveRestart(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"lost Telegram response", "interrupted reserved send"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := newKnowledgeFixture(t)
			knowledgeNoticeSuggestion(t, f)
			require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
			rows := knowledgeNotices(t, f)
			require.Len(t, rows, 1)
			if kind == "lost Telegram response" {
				f.h.peers.loseSend = true
			} else {
				_, err := f.h.store.ReserveKnowledgeNotification(
					t.Context(),
					f.moderator,
					rows[0].ID,
					rows[0].Version,
					json.RawMessage(`{"chat_id":41002,"text":"synthetic reservation"}`),
				)
				require.NoError(t, err)
			}
			startKnowledgeDelivery(t, f)
			awaitKnowledgeNotice(t, f, rows[0].ID, "delivery_unknown")
			f.h.stop(t)
			f.h.start(t)
			want := 0
			if kind == "lost Telegram response" {
				want = 1
			}
			require.Never(
				t,
				func() bool { return len(sentNotifications(f.h.peers, 41002)) > want },
				250*time.Millisecond,
				25*time.Millisecond,
			)
			require.Len(t, knowledgeNotices(t, f), 1)
		})
	}
}

func TestKnowledgeNoticesThrottledRetryKeepsOriginalNoticeAfterRestart(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	knowledgeNoticeSuggestion(t, f)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	rows := knowledgeNotices(t, f)
	require.Len(t, rows, 1)
	f.h.peers.rejectSendOnce = true
	startKnowledgeDelivery(t, f)
	awaitKnowledgeNotice(t, f, rows[0].ID, "retryable")
	f.h.stop(t)
	f.h.start(t)
	delivered := awaitKnowledgeNotice(t, f, rows[0].ID, "delivered")
	require.EqualValues(t, 5, delivered.Version)
	require.Len(t, sentNotifications(f.h.peers, 41002), 2)
	require.Len(t, knowledgeNotices(t, f), 1)
}

func TestKnowledgeNoticesRepairIsBoundedAndIncludesLaterModerators(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	knowledgeNoticeSuggestion(t, f)
	_, err := f.h.store.Pool().Exec(t.Context(), `WITH users AS (
 INSERT INTO shuttle_users(id,bot_id,tg_user_id,zitadel_subject,zitadel_org)
 SELECT gen_random_uuid(),123,50000+n,'late-notice-'||n,'org' FROM generate_series(1,120) n RETURNING id)
 INSERT INTO shuttle_knowledge_moderators(bot_id,organization_id,user_id,enabled,version,changed_by)
 SELECT 123,'org',id,true,1,$1 FROM users`, f.admin.UserID)
	require.NoError(t, err)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	var count int
	require.NoError(
		t,
		f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_knowledge_notices").Scan(&count),
	)
	require.Equal(t, 100, count)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.admin.EventScope))
	require.NoError(
		t,
		f.h.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_knowledge_notices").Scan(&count),
	)
	require.Equal(t, 121, count)
	cursor := uuid.Nil
	seen := map[uuid.UUID]bool{}
	for {
		page, e := f.h.store.ListKnowledgeNotifications(t.Context(), f.admin, cursor, 20)
		require.NoError(t, e)
		for _, n := range page.Items {
			require.False(t, seen[n.ID])
			seen[n.ID] = true
			require.NotEqual(t, f.user.UserID, n.RecipientID)
		}
		if page.Next == nil {
			break
		}
		cursor = *page.Next
	}
	require.Len(t, seen, 121)
	own, err := f.h.store.ListKnowledgeNotifications(t.Context(), f.moderator, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, own.Items, 1)
	empty, err := f.h.store.ListKnowledgeNotifications(t.Context(), f.other, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, empty.Items)
}

func TestKnowledgeNoticeMetadataAPIIsActorScopedAndRedacted(t *testing.T) {
	t.Parallel()
	f := newKnowledgeHTTPFixture(t)
	content := knowledgeContent()
	knowledgeApply(
		t,
		knowledgeFixture{h: f.h},
		f.ownerScope,
		db.KnowledgeChange{Kind: "suggest", ArticleID: uuid.New(), Content: &content, Reason: "Metadata redaction"},
	)
	require.NoError(t, f.h.store.EnsureKnowledgeNotifications(t.Context(), f.ownerScope.EventScope))
	read := func(scope db.PaymentScope) []byte {
		return knowledgeAgentCall(
			t,
			f,
			scope,
			"notifications",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     uuid.NewString(),
				"arguments":        map[string]any{"limit": 20},
			},
			http.StatusOK,
		)
	}
	raw := read(f.reviewerScope)
	var body struct {
		Result db.KnowledgeNotificationPage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &body))
	require.Len(t, body.Result.Items, 1)
	require.NotContains(t, string(raw), content.Body)
	require.NotContains(t, string(raw), content.Title)
	require.NotContains(t, string(raw), "payload")
	require.NotContains(t, string(raw), "attempt_id")
	require.NoError(t, json.Unmarshal(read(f.ownerScope), &body))
	require.Empty(t, body.Result.Items)
}
