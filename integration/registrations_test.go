package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func registrationUser(t *testing.T, h *harness, tgID int64, admin bool) (uuid.UUID, string) {
	t.Helper()
	id := uuid.New()
	subject := "registration-" + id.String()
	_, err := h.store.Pool().
		Exec(context.Background(), "INSERT INTO shuttle_users(id,bot_id,tg_user_id,profile,zitadel_subject,zitadel_org) VALUES($1,123,$2,'{}',$3,'org')", id, tgID, subject)
	require.NoError(t, err)
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	h.peers.adminRole = admin
	return id, h.peers.token(subject)
}
func registrationEvent(t *testing.T, h *harness, operator string) events.Event {
	t.Helper()
	closeAt := time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)
	doc := events.Document{
		Name:     "Registration fixture",
		Timezone: "Europe/Minsk",
		StartsAt: closeAt.Add(time.Hour),
		EndsAt:   closeAt.Add(48 * time.Hour),
		Modules:  []string{"registrations", "orders"},
		Registration: &registrations.Rules{
			LeaderCapacity:       1,
			FollowerCapacity:     1,
			PriceMinor:           12345,
			Currency:             "BYN",
			CurrencyScale:        2,
			ClosesAt:             closeAt,
			PaymentWindowSeconds: 7200,
		},
	}
	id := uuid.NewString()
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+id,
		map[string]any{"operation_id": "create-event-" + id, "expected_version": 0, "document": doc},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var event events.Event
	require.NoError(t, json.Unmarshal(data, &event))
	return event
}

func registerForEvent(
	t *testing.T,
	h *harness,
	token string,
	eventID uuid.UUID,
	operation, role string,
) registrations.Registration {
	t.Helper()
	code, data := adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		"/internal/events/"+eventID.String()+"/registration",
		map[string]any{"operation_id": operation, "role": role},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var result registrations.Registration
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}
func assignmentBody(operation string, event events.Event, entry registrations.Registration) map[string]any {
	return map[string]any{
		"operation_id":           operation,
		"expected_version":       entry.Version,
		"expected_event_version": event.Version,
	}
}
func assignmentPath(eventID, entryID uuid.UUID) string {
	return "/internal/admin/events/" + eventID.String() + "/registrations/" + entryID.String() + "/assign"
}

func TestSoloRegistrationAndPassSurviveRestartWithoutDuplicateEffects(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	_, operator := registrationUser(t, h, 9101, true)
	userID, userToken := registrationUser(t, h, 9102, false)
	event := registrationEvent(t, h, operator)
	scope := db.EventScope{BotID: 123, Organization: "org"}
	entry := registerForEvent(t, h, userToken, event.ID, "join", registrations.Leader)
	require.Equal(t, userID, entry.UserID)
	require.Equal(t, registrations.Waitlisted, entry.State)
	require.Nil(t, entry.Pass)
	require.Equal(t, entry, registerForEvent(t, h, userToken, event.ID, "join", registrations.Leader))
	require.Equal(t, entry, registerForEvent(t, h, userToken, event.ID, "join-again", registrations.Leader))
	allowed, err := h.store.ModuleAvailable(context.Background(), scope, event.ID, userID, "orders")
	require.NoError(t, err)
	require.False(t, allowed)
	body := assignmentBody("assign", event, entry)
	code, data := adminRequest(t, h, operator, http.MethodPost, assignmentPath(event.ID, entry.ID), body)
	require.Equal(t, http.StatusOK, code, string(data))
	var assigned registrations.Registration
	require.NoError(t, json.Unmarshal(data, &assigned))
	require.Equal(t, registrations.Assigned, assigned.State)
	require.EqualValues(t, 2, assigned.Version)
	require.NotNil(t, assigned.Pass)
	require.EqualValues(t, 12345, assigned.Pass.PriceMinor)
	require.Equal(t, event.Document.Registration.ClosesAt, assigned.Pass.PaymentDueAt)
	allowed, err = h.store.ModuleAvailable(context.Background(), scope, event.ID, userID, "orders")
	require.NoError(t, err)
	require.True(t, allowed)
	allowed, err = h.store.ModuleAvailable(context.Background(), scope, event.ID, userID, "massage")
	require.NoError(t, err)
	require.False(t, allowed)
	h.stop(t)
	h.start(t)
	code, data = adminRequest(t, h, operator, http.MethodPost, assignmentPath(event.ID, entry.ID), body)
	require.Equal(t, http.StatusOK, code, string(data))
	var replay registrations.Registration
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, assigned, replay)
	event.Document.Registration.PriceMinor = 54321
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{"operation_id": "change-price", "expected_version": 1, "document": event.Document},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	code, data = adminRequest(
		t,
		h,
		userToken,
		http.MethodGet,
		"/internal/events/"+event.ID.String()+"/registration",
		nil,
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, assigned, replay)
	code, data = adminRequest(t, h, userToken, http.MethodGet, "/internal/registration-operations/join", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	replay = registrations.Registration{}
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, entry, replay)
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodGet,
		"/internal/admin/events/"+event.ID.String()+"/registrations?limit=1",
		nil,
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), entry.ID.String())
	code, _ = adminRequest(
		t,
		h,
		userToken,
		http.MethodPost,
		"/internal/events/"+event.ID.String()+"/registration",
		map[string]any{"operation_id": "join", "role": "follower"},
	)
	require.Equal(t, http.StatusConflict, code)
}

func TestRegistrationAssignmentSerializesCapacityAndEnforcesRights(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	_, operator := registrationUser(t, h, 9201, true)
	firstID, first := registrationUser(t, h, 9202, false)
	_, second := registrationUser(t, h, 9203, false)
	event := registrationEvent(t, h, operator)
	a := registerForEvent(t, h, first, event.ID, "join", registrations.Leader)
	b := registerForEvent(t, h, second, event.ID, "join", registrations.Leader)
	code, _ := adminRequest(
		t,
		h,
		first,
		http.MethodPost,
		assignmentPath(event.ID, a.ID),
		assignmentBody("denied", event, a),
	)
	require.Equal(t, http.StatusForbidden, code)
	code, _ = adminRequest(t, h, second, http.MethodGet, "/internal/registration-operations/denied", nil)
	require.Equal(t, http.StatusNotFound, code)
	stale := assignmentBody("stale", event, a)
	stale["expected_event_version"] = 2
	code, _ = adminRequest(t, h, operator, http.MethodPost, assignmentPath(event.ID, a.ID), stale)
	require.Equal(t, http.StatusConflict, code)
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for _, entry := range []registrations.Registration{a, b} {
		wg.Go(func() {
			status, _ := adminRequest(
				t,
				h,
				operator,
				http.MethodPost,
				assignmentPath(event.ID, entry.ID),
				assignmentBody("assign-"+entry.ID.String(), event, entry),
			)
			results <- status
		})
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for status := range results {
		switch status {
		case http.StatusOK:
			success++
		case http.StatusConflict:
			conflict++
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflict)
	code, data := adminRequest(t, h, first, http.MethodGet, "/internal/events/"+event.ID.String()+"/registration", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var own registrations.Registration
	require.NoError(t, json.Unmarshal(data, &own))
	require.Equal(t, firstID, own.UserID)
	code, _ = adminRequest(
		t,
		h,
		first,
		http.MethodGet,
		"/internal/admin/events/"+event.ID.String()+"/registrations",
		nil,
	)
	require.Equal(t, http.StatusForbidden, code)
	_, err := h.store.SetUserRevoked(context.Background(), firstID, "fixture")
	require.NoError(t, err)
	code, _ = adminRequest(t, h, first, http.MethodGet, "/internal/events/"+event.ID.String()+"/registration", nil)
	require.Equal(t, http.StatusForbidden, code)
	allowed, err := h.store.ModuleAvailable(
		context.Background(),
		db.EventScope{BotID: 123, Organization: "org"},
		event.ID,
		firstID,
		"orders",
	)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestRegistrationClosingAndModuleDisableRejectNewActionsButPreserveReceipts(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	_, operator := registrationUser(t, h, 9301, true)
	_, user := registrationUser(t, h, 9302, false)
	event := registrationEvent(t, h, operator)
	entry := registerForEvent(t, h, user, event.ID, "join", registrations.Follower)
	event.Document.Registration.ClosesAt = time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{"operation_id": "close", "expected_version": 1, "document": event.Document},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, entry, registerForEvent(t, h, user, event.ID, "join", registrations.Follower))
	code, _ = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/events/"+event.ID.String()+"/registration",
		map[string]any{"operation_id": "new", "role": "follower"},
	)
	require.Equal(t, http.StatusConflict, code)
	event.Version = 2
	code, _ = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		assignmentPath(event.ID, entry.ID),
		assignmentBody("late", event, entry),
	)
	require.Equal(t, http.StatusConflict, code)
	event.Document.Registration.ClosesAt = time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	event.Document.Modules = []string{"orders"}
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{"operation_id": "disable", "expected_version": 2, "document": event.Document},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	code, _ = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/events/"+event.ID.String()+"/registration",
		map[string]any{"operation_id": "disabled", "role": "follower"},
	)
	require.Equal(t, http.StatusConflict, code)
}
