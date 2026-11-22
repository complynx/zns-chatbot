package integration_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/broadcasts"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func communicationClick(t *testing.T, f knowledgeFixture, user int64, data string, update int64) {
	t.Helper()
	u := telegram.Update{
		ID: update,
		CallbackQuery: &telegram.CallbackQuery{
			ID:      uuid.NewString(),
			From:    telegram.User{ID: user},
			Message: &telegram.Message{ID: 1, Chat: telegram.Chat{ID: user, Type: "private"}},
			Data:    data,
		},
	}
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	task, _, err := f.h.service.Accept(t.Context(), u, raw)
	require.NoError(t, err)
	f.h.await(t, task.ID, "completed")
}

func communicationRecipientMessage(
	t *testing.T,
	f knowledgeFixture,
	chat int64,
	contains string,
) telegram.SendMessageRequest {
	t.Helper()
	var result telegram.SendMessageRequest
	require.Eventually(t, func() bool {
		f.h.peers.mu.Lock()
		defer f.h.peers.mu.Unlock()
		for _, m := range f.h.peers.sent {
			if m.ChatID == chat && strings.Contains(m.Text, contains) {
				result = m
				return true
			}
		}
		return false
	}, 30*time.Second, 20*time.Millisecond)
	return result
}

func TestCommunicationsNativeConsentDeliverySubscriptionsAndServiceBilling(t *testing.T) {
	t.Parallel()
	for _, personalized := range []bool{false, true} {
		name := "literal"
		if personalized {
			name = "personalized"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			communicationDeliveryScenario(t, personalized)
		})
	}
}

func TestCommunicationSpecialistOnlyOwnBookedClients(t *testing.T) {
	t.Parallel()
	f, staff, schedule, binding := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	other := ownerProfileScope(t, f.H, f.AmbassadorID)
	bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	otherBooking := massageBookingArgs(f, schedule, 0)
	otherBooking.Selection.SpecialistID = "beth"
	bookMassage(t, f, other, otherBooking)
	scope := db.PaymentScope{
		BotID:        staff.BotID,
		Organization: staff.Organization,
		Subject:      staff.Subject,
		UserID:       staff.UserID,
	}
	a := db.CommunicationArguments{
		EventID:    f.Event.ID,
		Recipients: []uuid.UUID{owner.UserID},
		Category:   "personal",
		Text:       "Appointment details",
	}
	_, err := f.H.store.PrepareCommunication(t.Context(), scope, uuid.New(), a, false)
	require.NoError(t, err)
	a.Recipients = []uuid.UUID{other.UserID}
	_, err = f.H.store.PrepareCommunication(t.Context(), scope, uuid.New(), a, false)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
	page, err := f.H.store.CommunicationAudience(t.Context(), scope, db.CommunicationAudienceQuery{EventID: f.Event.ID})
	require.NoError(t, err)
	require.Len(t, page.Users, 1)
	require.Equal(t, owner.UserID, page.Users[0].UserID)
	binding.Active = false
	bindingChange(t, f, staff, binding)
	a.Recipients = []uuid.UUID{owner.UserID}
	_, err = f.H.store.PrepareCommunication(t.Context(), scope, uuid.New(), a, false)
	require.ErrorIs(t, err, db.ErrPeopleDenied)
}

func TestCommunicationOrdinaryCallerToVerifiedGlobalAdmin(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	target, _ := registrationUser(t, h, 41501, false)
	_, callerToken := registrationUser(t, h, 41502, false)
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), `UPDATE shuttle_users SET visible=false,
 profile='{"username":"global_contact"}' WHERE id=$1 RETURNING id`, target).Scan(&target))
	code, raw := peopleRequest(t, h, callerToken, "resolve", map[string]any{"username": "global_contact"})
	require.Equal(t, http.StatusOK, code, string(raw))
	p.mu.Lock()
	p.adminRole = true
	p.mu.Unlock()
	a := db.CommunicationArguments{
		Recipients: []uuid.UUID{target},
		Category:   "personal",
		Text:       "Question for the administrator",
	}
	request := func() (int, []byte) {
		op := uuid.NewString()
		return adminRequest(t, h, callerToken, http.MethodPost, "/internal/agent/communications/prepare",
			map[string]any{"protocol_version": 1, "operation_id": op, "idempotency_key": op,
				"confirmation": map[string]any{"mode": "none"}, "arguments": a})
	}
	code, raw = request()
	require.Equal(t, http.StatusOK, code, string(raw))
	p.mu.Lock()
	p.adminRole = false
	p.mu.Unlock()
	u, err := h.store.GetUser(t.Context(), target)
	require.NoError(t, err)
	h.iam.Invalidate(u.Subject)
	code, raw = request()
	require.Equal(t, http.StatusForbidden, code, string(raw))
}

func TestCommunicationPreparationRecoveryByOperation(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	_, authorToken := registrationUser(t, h, 41601, true)
	target, otherToken := registrationUser(t, h, 41602, false)
	op := uuid.NewString()
	a := db.CommunicationArguments{Recipients: []uuid.UUID{target}, Category: "personal", Text: "Exact draft"}
	envelope := map[string]any{"protocol_version": 1, "operation_id": op, "run_id": uuid.NewString(),
		"idempotency_key": op, "confirmation": map[string]any{"mode": "none"}, "arguments": a}
	code, raw := adminRequest(t, h, authorToken, http.MethodPost, "/internal/agent/communications/prepare",
		envelope)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared struct {
		Status      string           `json:"status"`
		OperationID string           `json:"operation_id"`
		Result      db.Communication `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &prepared))
	require.Equal(t, "succeeded", prepared.Status)
	require.Equal(t, op, prepared.OperationID)
	code, raw = adminRequest(t, h, authorToken, http.MethodPost, "/internal/agent/communications/prepare", envelope)
	require.Equal(t, http.StatusOK, code, string(raw))
	var replay struct {
		Result db.Communication `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &replay))
	require.Equal(t, prepared.Result.ID, replay.Result.ID)
	for _, invalid := range []map[string]any{
		{"idempotency_key": uuid.NewString()},
		{"confirmation": map[string]any{"mode": "exact_parameters"}},
	} {
		bad := maps.Clone(envelope)
		maps.Copy(bad, invalid)
		code, raw = adminRequest(t, h, authorToken, http.MethodPost, "/internal/agent/communications/prepare", bad)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	for _, test := range []struct{ token, operation, status string }{
		{authorToken, op, "succeeded"}, {otherToken, op, "not_found"}, {authorToken, uuid.NewString(), "not_found"},
	} {
		code, raw = adminRequest(t, h, test.token, http.MethodPost, "/internal/agent/communications/prepare/status",
			map[string]any{"protocol_version": 1, "operation_id": test.operation})
		require.Equal(t, http.StatusOK, code, string(raw))
		var found struct {
			Status      string            `json:"status"`
			OperationID string            `json:"operation_id"`
			Result      *db.Communication `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &found))
		require.Equal(t, test.status, found.Status)
		require.Equal(t, test.operation, found.OperationID)
		if test.status == "succeeded" {
			require.NotNil(t, found.Result)
			require.Equal(t, prepared.Result.ID, found.Result.ID)
			require.Equal(t, a.Text, found.Result.Arguments.Text)
		} else {
			require.Nil(t, found.Result)
		}
	}
	var deliveries int
	require.NoError(t, h.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_communication_deliveries
 WHERE communication_id=$1 AND kind='message'`, prepared.Result.ID).Scan(&deliveries))
	require.Zero(t, deliveries, "status recovery does not confirm or send the draft")
	a.Personalized = true
	a.Category, a.AssessedCategory, a.Assessment = "information", broadcasts.Information, "Event update"
	missingMachineOp := uuid.NewString()
	envelope["operation_id"], envelope["idempotency_key"], envelope["arguments"] = missingMachineOp, missingMachineOp, a
	code, raw = adminRequest(t, h, authorToken, http.MethodPost, "/internal/agent/communications/prepare", envelope)
	require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
	var drafts int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), `SELECT count(*) FROM shuttle_communications WHERE operation_id=$1`, missingMachineOp).
			Scan(&drafts),
	)
	require.Zero(t, drafts, "missing billing configuration must not create a draft or confirmation")
}

type communicationPeerStats struct{ models, memory, reads atomic.Int32 }

func TestCommunicationCompletedRewriteSurvivesDeliveryDeadline(t *testing.T) {
	t.Parallel()
	f := newKnowledgeFixture(t)
	f.h.peers.adminRole = true
	var unexpected, cancels atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/runs/expired-rewrite/cancel" {
			cancels.Add(1)
			assert.Equal(t, "Bearer personalization-service", r.Header.Get("Authorization"))
			assert.Empty(t, r.Header.Get("X-Subject-Token"))
			respond(w, loom.ControlReceipt{ID: "expired-rewrite", Status: "cancelled", Version: 2})
			return
		}
		unexpected.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer proxy.Close()
	f.h.peers.loom = proxy
	var drafts []uuid.UUID
	for _, text := range []string{"Cached personalized body", ""} {
		a := db.CommunicationArguments{Recipients: []uuid.UUID{f.user.UserID}, Category: "information",
			AssessedCategory: broadcasts.Information, Assessment: "Event update", Text: "Original body", Personalized: true}
		c, err := f.h.store.PrepareCommunication(t.Context(), f.admin, uuid.New(), a, false)
		require.NoError(t, err)
		_, err = f.h.store.DecideCommunication(t.Context(), f.admin, c.ID, "keep", false)
		require.NoError(t, err)
		_, err = f.h.store.Pool().Exec(t.Context(), `UPDATE shuttle_communication_deliveries SET state='retryable',
 personalization_started_at=now()-interval '31 minutes',rendered_text=nullif($2,''),
 personalization_run_id=CASE WHEN $2='' THEN 'expired-rewrite' ELSE 'completed-rewrite' END
 WHERE communication_id=$1 AND kind='message'`, c.ID, text)
		require.NoError(t, err)
		drafts = append(drafts, c.ID)
	}
	f.h.start(t)
	defer f.h.stop(t)
	communicationRecipientMessage(t, f, 41003, "Cached personalized body")
	require.Eventually(t, func() bool {
		complete, e := f.h.store.Communication(t.Context(), f.admin, drafts[0])
		expired, other := f.h.store.Communication(t.Context(), f.admin, drafts[1])
		return e == nil && other == nil && complete.DeliveryCounts["delivered"] == 1 &&
			expired.DeliveryCounts["delivery_failed"] == 1
	}, 10*time.Second, 20*time.Millisecond)
	require.Zero(t, unexpected.Load(), "cached text needs no memory, chat, run or status request")
	require.EqualValues(t, 1, cancels.Load(), "an expired unfinished run is cancelled")
}

type communicationFailureStats struct{ creates, chats, memory, cancels atomic.Int32 }

func communicationFailurePeer(t *testing.T, p *peers, name string) (*httptest.Server, *communicationFailureStats) {
	t.Helper()
	stats := &communicationFailureStats{}
	original := p.loom.Config.Handler
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/runs/suspended-run/cancel":
			stats.cancels.Add(1)
			assert.Empty(t, r.Header.Get("X-Subject-Token"))
			assert.Equal(t, "Bearer personalization-service", r.Header.Get("Authorization"))
			respond(w, loom.ControlReceipt{ID: "suspended-run", Status: "cancelled", Version: 2})
		case "/v1/chats":
			stats.chats.Add(1)
			if name == "chat_unknown" {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			respond(w, map[string]string{"id": uuid.NewString()})
		case "/v1/users/me/memory":
			stats.memory.Add(1)
			respond(w, map[string]any{"micro": map[string]string{"text": "private memory"}})
		case "/v1/capabilities":
			respond(w, map[string]any{"execution_profiles": []string{"native_files"}})
		case "/v1/runs":
			stats.creates.Add(1)
			if name == "rejected" {
				// Loom gives an error code with each rejection.
				rejectLoom(w, http.StatusForbidden, "forbidden")
				return
			}
			if name == "injected_link" {
				respond(w, loom.Run{ID: "injected-run", Status: "completed", Result: "https://attacker.example/?secret=private",
					Interaction: loom.Interaction{ReplyMode: loom.ReplyModeAgent, Questions: "none"}})
				return
			}
			respond(
				w,
				loom.Run{
					ID:          "suspended-run",
					Status:      "paused",
					Reason:      "budget_exhausted",
					Interaction: loom.Interaction{ReplyMode: loom.ReplyModeAgent, Questions: "none"},
				},
			)
		default:
			original.ServeHTTP(w, r)
		}
	}))
	return proxy, stats
}

func TestCommunicationPersonalizationOutcomes(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"rejected", "suspended", "chat_unknown", "injected_link"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newKnowledgeFixture(t)
			p := f.h.peers
			p.adminRole = true
			proxy, stats := communicationFailurePeer(t, p, name)
			defer proxy.Close()
			p.loom = proxy
			a := db.CommunicationArguments{
				Recipients:       []uuid.UUID{f.user.UserID},
				Text:             "Event update",
				Category:         "information",
				AssessedCategory: broadcasts.Information,
				Assessment:       "Logistics",
				Personalized:     true,
			}
			c, err := f.h.store.PrepareCommunication(t.Context(), f.admin, uuid.New(), a, false)
			require.NoError(t, err)
			_, err = f.h.store.DecideCommunication(t.Context(), f.admin, c.ID, "keep", false)
			require.NoError(t, err)
			f.h.start(t)
			defer f.h.stop(t)
			outcome := "delivery_failed"
			if name == "injected_link" {
				outcome = "delivered"
			}
			require.Eventually(t, func() bool {
				status, e := f.h.store.Communication(t.Context(), f.admin, c.ID)
				return e == nil && status.DeliveryCounts[outcome] == 1
			}, 10*time.Second, 20*time.Millisecond)
			require.EqualValues(t, 1, stats.chats.Load())
			if name == "chat_unknown" {
				require.Zero(t, stats.creates.Load())
				require.Zero(t, stats.memory.Load())
			} else {
				require.EqualValues(t, 1, stats.creates.Load())
			}
			if name == "injected_link" {
				m := communicationRecipientMessage(t, f, 41003, a.Text)
				require.True(t, strings.HasSuffix(m.Text, a.Text))
				require.NotContains(t, m.Text, "attacker.example")
				require.NotNil(t, m.LinkPreviewOptions)
				require.True(t, m.LinkPreviewOptions.IsDisabled)
			} else {
				require.Empty(t, sentNotifications(p, 41003))
			}
			if name == "suspended" {
				require.EqualValues(t, 1, stats.cancels.Load())
			} else {
				require.Zero(t, stats.cancels.Load())
			}
			var cleared bool
			require.NoError(
				t,
				f.h.store.Pool().
					QueryRow(t.Context(), `SELECT personalization_input IS NULL AND rendered_text IS NULL
 FROM shuttle_communication_deliveries WHERE communication_id=$1 AND kind='message'`, c.ID).
					Scan(&cleared),
			)
			require.True(t, cleared)
		})
	}
}

func communicationLoomPeer(t *testing.T, p *peers) (*httptest.Server, *communicationPeerStats) {
	t.Helper()
	stats := &communicationPeerStats{}
	original := p.loom.Config.Handler
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/chats":
			assert.Equal(t, "Bearer personalization-service", r.Header.Get("Authorization"))
			respond(w, map[string]string{"id": uuid.NewString()})
		case "/v1/users/me/memory":
			token, err := jwt.ParseSigned(
				strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
				[]jose.SignatureAlgorithm{jose.ES256},
			)
			if assert.NoError(t, err) {
				var claims jwt.Claims
				assert.NoError(t, token.Claims(&p.key.PublicKey, &claims))
				assert.Equal(t, "actor", claims.Subject, "memory must use the original delegation actor")
			}
			if stats.memory.Add(1) > 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			assert.NotEmpty(t, r.Header.Get("X-Subject-Token"))
			assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
			respond(
				w,
				map[string]any{
					"micro": map[string]string{"text": "Prefers concise Russian. Private detail must not be disclosed."},
					"index": map[string]string{"text": ""},
				},
			)
		case "/v1/capabilities":
			assert.Equal(t, "Bearer personalization-service", r.Header.Get("Authorization"))
			respond(w, map[string]any{"execution_profiles": []string{"native_files"}})
		case "/v1/runs":
			assert.Equal(t, "Bearer personalization-service", r.Header.Get("Authorization"))
			assert.Empty(t, r.Header.Get("X-Subject-Token"), "recipient must not pay for inference")
			assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
			var request loom.CreateRunRequest
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&request))
			assert.Contains(t, request.Input.Text, "Prefers concise Russian")
			assert.Equal(t, "native_files", request.ExecutionProfile)
			_, parseErr := uuid.Parse(request.ChatID)
			assert.NoError(t, parseErr, "each rewrite must use an isolated chat")
			assert.Equal(t, "none", request.Interaction.Questions)
			stats.models.Add(1)
			respond(
				w,
				loom.Run{
					ID:          "personalization-run",
					Status:      "running",
					Version:     1,
					Interaction: request.Interaction,
				},
			)
		case "/v1/runs/personalization-run":
			assert.Equal(t, "Bearer personalization-service", r.Header.Get("Authorization"))
			assert.Empty(t, r.Header.Get("X-Subject-Token"))
			run := loom.Run{ID: "personalization-run", Status: "succeeded", Result: "Встречаемся в 18:00.", Version: 5}
			stats.reads.Add(1)
			respond(w, run)
		default:
			original.ServeHTTP(w, r)
		}
	}))
	return proxy, stats
}

func communicationDeliveryScenario(t *testing.T, personalized bool) {
	t.Helper()

	f := newKnowledgeFixture(t)
	p := f.h.peers
	p.adminRole = true
	if personalized {
		p.markupEdits = []telegram.EditMessageMarkupRequest{}
		_, err := f.h.store.Pool().Exec(t.Context(),
			`INSERT INTO shuttle_user_profiles(user_id,version,full_name) VALUES($1,1,'Saved Sender')`, f.admin.UserID)
		require.NoError(t, err)
	}
	proxy, stats := communicationLoomPeer(t, p)

	defer proxy.Close()
	p.loom = proxy
	a := db.CommunicationArguments{
		Recipients:       []uuid.UUID{f.user.UserID},
		Text:             "Meet at 18:00. **Exact**",
		Category:         "warning",
		AssessedCategory: broadcasts.Information,
		Assessment:       "Meeting time, no concrete risk",
		Personalized:     personalized,
	}
	draft, err := f.h.store.PrepareCommunication(t.Context(), f.admin, uuid.New(), a, false)
	require.NoError(t, err)
	f.h.start(t)
	defer f.h.stop(t)
	prompt := communicationRecipientMessage(t, f, 41001, "Meeting time")
	keyboard, ok := prompt.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.True(t, ok)
	require.Len(t, keyboard.InlineKeyboard, 3)
	require.Empty(t, sentNotifications(p, 41003), "draft must not reach the recipient before confirmation")
	communicationClick(t, f, 41001, keyboard.InlineKeyboard[1][0].CallbackData, 51001)
	body := a.Text
	if personalized {
		body = "Встречаемся в 18:00."
	}
	message := communicationRecipientMessage(t, f, 41003, body)
	require.True(t, strings.HasSuffix(message.Text, body))
	if personalized {
		require.Contains(t, message.Text, "Saved Sender")
		require.NotNil(t, message.LinkPreviewOptions)
		require.True(t, message.LinkPreviewOptions.IsDisabled)
	} else {
		require.Contains(t, message.Text, "Telegram #41001")
	}
	keyboard, ok = message.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.True(t, ok)
	require.Len(t, keyboard.InlineKeyboard, 2)
	if personalized {
		editBroadcastControlsInPlace(t, f, message, keyboard)
	} else {
		communicationClick(t, f, 41003, keyboard.InlineKeyboard[1][0].CallbackData, 51002)
		manage := communicationRecipientMessage(t, f, 41003, "Unsubscribing may hide important event information")
		management := manage.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
		communicationClick(t, f, 41003, management.InlineKeyboard[0][0].CallbackData, 51003)
		restore := communicationRecipientMessage(t, f, 41003, "Restore with the button")
		require.NotNil(t, restore.ReplyMarkup)
	}
	if personalized {
		require.EqualValues(t, 1, stats.models.Load())
		require.EqualValues(t, 1, stats.memory.Load())
		require.EqualValues(t, 1, stats.reads.Load())
	} else {
		require.Zero(t, stats.models.Load())
	}
	status, err := f.h.store.Communication(t.Context(), f.admin, draft.ID)
	require.NoError(t, err)
	require.Equal(t, "information", status.Category)
	require.Equal(t, 1, status.DeliveryCounts["delivered"])
	if personalized {
		publicStatus, marshalErr := json.Marshal(status)
		require.NoError(t, marshalErr)
		require.NotContains(t, string(publicStatus), "Prefers concise Russian")
		require.NotContains(t, string(publicStatus), body)
		var cleared bool
		require.NoError(
			t,
			f.h.store.Pool().QueryRow(t.Context(), `SELECT personalization_input IS NULL AND rendered_text IS NULL
 FROM shuttle_communication_deliveries WHERE communication_id=$1 AND kind='message'`, draft.ID).Scan(&cleared),
		)
		require.True(t, cleared)
	}
	var telegramMessageID int64
	require.NoError(t, f.h.store.Pool().QueryRow(t.Context(),
		`SELECT (receipt->>'message_id')::bigint FROM shuttle_communication_deliveries
 WHERE communication_id=$1 AND kind='message'`, draft.ID).Scan(&telegramMessageID))
	routing, err := f.h.store.CommunicationReplyContext(t.Context(), db.ProfileScope{
		BotID: 123, Organization: "org", Subject: f.user.Subject, UserID: f.user.UserID,
	}, telegramMessageID)
	require.NoError(t, err)
	require.Equal(t, f.admin.UserID, routing.AuthorID)
	_, err = f.h.store.CommunicationReplyContext(t.Context(), db.ProfileScope{
		BotID: 123, Organization: "org", Subject: f.admin.Subject, UserID: f.admin.UserID,
	}, telegramMessageID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// Opted-out users are excluded before a personalized run is created.
	a.Category = "information"
	second, err := f.h.store.PrepareCommunication(t.Context(), f.admin, uuid.New(), a, false)
	require.NoError(t, err)
	_, err = f.h.store.DecideCommunication(t.Context(), f.admin, second.ID, "keep", false)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		current, e := f.h.store.Communication(t.Context(), f.admin, second.ID)
		return e == nil && current.DeliveryCounts["delivery_blocked"] == 1
	}, 15*time.Second, 25*time.Millisecond)
	if personalized {
		require.EqualValues(t, 1, stats.models.Load())
	}
	// Warnings are delivered despite that opt-out and have no unsubscribe controls.
	if personalized {
		deniedBroadcastClick(t, f, telegramMessageID, keyboard)
		return
	}
	a.Category = "warning"
	a.AssessedCategory = broadcasts.Warning
	a.Assessment = "Safety risk"
	a.Personalized = false
	a.Text = "Warning: safety instructions"
	warning, err := f.h.store.PrepareCommunication(t.Context(), f.admin, uuid.New(), a, false)
	require.NoError(t, err)
	_, err = f.h.store.DecideCommunication(t.Context(), f.admin, warning.ID, "keep", false)
	require.NoError(t, err)
	warningMessage := communicationRecipientMessage(t, f, 41003, a.Text)
	require.Nil(t, warningMessage.ReplyMarkup)
}

func editBroadcastControlsInPlace(
	t *testing.T, f knowledgeFixture, message telegram.SendMessageRequest, keyboard *telegram.InlineKeyboardMarkup,
) {
	t.Helper()
	p := f.h.peers
	var messageID int64
	p.mu.Lock()
	for n, sent := range p.sent {
		if sent.ChatID == message.ChatID && sent.Text == message.Text {
			messageID = int64(n + 1)
		}
	}
	p.mu.Unlock()
	require.Positive(t, messageID)
	click := func(update int64, data string, count int) *telegram.InlineKeyboardMarkup {
		task := registrationButton(t, f.h, update, messageID, message.ChatID, data)
		f.h.await(t, task, "completed")
		require.Eventually(t, func() bool {
			p.mu.Lock()
			defer p.mu.Unlock()
			return len(p.markupEdits) == count
		}, 5*time.Second, 25*time.Millisecond)
		p.mu.Lock()
		defer p.mu.Unlock()
		edit := p.markupEdits[count-1]
		require.Equal(t, messageID, edit.MessageID)
		require.Equal(t, message.ChatID, edit.ChatID)
		require.Equal(t, message.Text, p.sent[messageID-1].Text)
		return edit.ReplyMarkup
	}
	global := click(51002, keyboard.InlineKeyboard[1][0].CallbackData, 1)
	require.Len(t, global.InlineKeyboard, 4)
	require.Contains(t, global.InlineKeyboard[0][0].Text, "May miss updates")
	event := click(51003, global.InlineKeyboard[2][0].CallbackData, 2)
	require.True(t, strings.HasSuffix(event.InlineKeyboard[0][0].CallbackData, ":offevent"))
	global = click(51004, event.InlineKeyboard[3][0].CallbackData, 3)
	require.True(t, strings.HasSuffix(global.InlineKeyboard[0][0].CallbackData, ":offall"))
	click(51005, global.InlineKeyboard[0][0].CallbackData, 4)
	require.Len(t, sentNotifications(p, message.ChatID), 1)
	var subscribed bool
	err := f.h.store.Pool().QueryRow(t.Context(), `SELECT subscribed
 FROM shuttle_broadcast_subscriptions WHERE user_id=$1 AND scope='all' AND category='information'`,
		f.user.UserID).Scan(&subscribed)
	require.NoError(t, err)
	require.False(t, subscribed)
}

func deniedBroadcastClick(t *testing.T, f knowledgeFixture, messageID int64, keyboard *telegram.InlineKeyboardMarkup) {
	t.Helper()
	const recipient = 41003
	p := f.h.peers
	p.mu.Lock()
	p.userInactive = true
	p.messageEdits = []telegram.EditMessageRequest{}
	p.mu.Unlock()
	f.h.iam.Invalidate(f.user.Subject)
	click := registrationButton(t, f.h, 51006, messageID, recipient, keyboard.InlineKeyboard[1][0].CallbackData)
	f.h.await(t, click, "failed")
	communicationRecipientMessage(t, f, recipient, "don’t have access right now")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.messageEdits)
	require.Len(t, p.markupEdits, 4)
}
