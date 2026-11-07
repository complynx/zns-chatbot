package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/events"
)

func contextEvent(t *testing.T, h *harness, operator string, start time.Time, zone string) events.Event {
	t.Helper()
	event := registrationEvent(t, h, operator)
	event.Document.StartsAt = start
	event.Document.EndsAt = start.Add(48 * time.Hour)
	event.Document.Timezone = zone
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+event.ID.String(),
		map[string]any{
			"operation_id":     "dates-" + event.ID.String(),
			"expected_version": event.Version,
			"document":         event.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &event))
	return event
}
func contextPass(t *testing.T, h *harness, operator, user string, event events.Event) {
	t.Helper()
	entry := registerForEvent(t, h, user, event.ID, "join-"+event.ID.String(), "leader")
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		assignmentPath(event.ID, entry.ID),
		assignmentBody("assign-"+event.ID.String(), event, entry),
	)
	require.Equal(t, http.StatusOK, code, string(data))
}

func resolveContext(
	t *testing.T,
	h *harness,
	token string,
	args events.ContextRequest,
	runID string,
) events.Context {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": "context-read", "arguments": args}
	if runID != "" {
		body["run_id"] = runID
	}
	code, data := adminRequest(t, h, token, http.MethodPost, "/internal/agent/context/resolve", body)
	require.Equal(t, http.StatusOK, code, string(data))
	return contextResult(t, data)
}
func contextResult(t *testing.T, data []byte) events.Context {
	t.Helper()
	var result struct {
		Status string         `json:"status"`
		Result events.Context `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, "succeeded", result.Status)
	return result.Result
}
func chooseContextBody(operation string, event events.Event, version int64, runID string) map[string]any {
	body := map[string]any{
		"protocol_version": 1,
		"operation_id":     operation,
		"arguments":        map[string]any{"module": "orders", "event_id": event.ID, "expected_version": version},
		"idempotency_key":  operation,
		"confirmation":     map[string]any{"mode": "none"},
	}
	if runID != "" {
		body["run_id"] = runID
	}
	return body
}

func TestEventContextUsesQualifiedNearestEventAndClarifiesAmbiguity(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9901, true)
	_, user := registrationUser(t, h, 9902, false)
	start := time.Now().UTC().Truncate(time.Microsecond).Add(24 * time.Hour)
	waitlisted := contextEvent(t, h, operator, start.Add(-time.Hour), "Europe/Moscow")
	registerForEvent(t, h, user, waitlisted.ID, "waitlisted", "leader")
	first := contextEvent(t, h, operator, start, "Europe/Minsk")
	contextPass(t, h, operator, user, first)
	selected := resolveContext(t, h, user, events.ContextRequest{Module: "orders"}, "")
	require.Equal(t, "nearest", selected.Source)
	require.Equal(t, first.ID, selected.Selected.ID)
	require.Equal(t, "Europe/Minsk", selected.Selected.Timezone)
	require.Equal(t, start, selected.Selected.StartsAt)
	// Waitlist has no orders entitlement but is an open registration context.
	registration := resolveContext(t, h, user, events.ContextRequest{Module: "registrations"}, "")
	require.Equal(t, waitlisted.ID, registration.Selected.ID)
	for _, invalid := range []any{uuid.Nil.String(), nil} {
		code, _ := adminRequest(
			t,
			h,
			user,
			http.MethodPost,
			"/internal/agent/context/resolve",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     "invalid-explicit",
				"arguments":        map[string]any{"module": "orders", "event_id": invalid},
			},
		)
		require.Equal(t, http.StatusUnprocessableEntity, code)
	}
	second := contextEvent(t, h, operator, start, "Asia/Yekaterinburg")
	contextPass(t, h, operator, user, second)
	code, page := adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/list",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "list",
			"arguments":        map[string]any{"module": "orders", "limit": 1},
		},
	)
	require.Equal(t, http.StatusOK, code, string(page))
	var listed struct {
		Result events.ContextList `json:"result"`
	}
	require.NoError(t, json.Unmarshal(page, &listed))
	require.Len(t, listed.Result.Events, 1)
	require.NotNil(t, listed.Result.Next)
	firstPageID := listed.Result.Events[0].ID
	code, page = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/list",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "next",
			"arguments":        map[string]any{"module": "orders", "limit": 1, "after": listed.Result.Next},
		},
	)
	require.Equal(t, http.StatusOK, code, string(page))
	listed.Result = events.ContextList{}
	require.NoError(t, json.Unmarshal(page, &listed))
	require.Len(t, listed.Result.Events, 1)
	require.Nil(t, listed.Result.Next)
	require.ElementsMatch(t, []uuid.UUID{first.ID, second.ID}, []uuid.UUID{firstPageID, listed.Result.Events[0].ID})
	ambiguous := resolveContext(t, h, user, events.ContextRequest{Module: "orders"}, "")
	require.Equal(t, "clarify", ambiguous.Source)
	require.Nil(t, ambiguous.Selected)
	require.Len(t, ambiguous.Choices, 2)
	hinted := resolveContext(t, h, user, events.ContextRequest{Module: "orders", HintID: &second.ID}, "")
	require.Equal(t, "hint", hinted.Source)
	require.Equal(t, second.ID, hinted.Selected.ID)
	explicit := resolveContext(
		t,
		h,
		user,
		events.ContextRequest{Module: "orders", EventID: &first.ID, HintID: &second.ID},
		"",
	)
	require.Equal(t, "explicit", explicit.Source)
	require.Equal(t, first.ID, explicit.Selected.ID)
	required := resolveContext(
		t,
		h,
		user,
		events.ContextRequest{Module: "orders", HintID: &second.ID, RequireExplicit: true},
		"",
	)
	require.Equal(t, "clarify", required.Source)
	require.Nil(t, required.Selected)
	require.Len(t, required.Choices, 2)
	code, _ = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/resolve",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     "inaccessible",
			"arguments":        events.ContextRequest{Module: "orders", EventID: &waitlisted.ID},
		},
	)
	require.Equal(t, http.StatusNotFound, code)
}

func TestSavedRunContextSurvivesRestartAndOtherRunSelection(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9911, true)
	userID, user := registrationUser(t, h, 456, false)
	_, otherUser := registrationUser(t, h, 9913, false)
	start := time.Now().UTC().Truncate(time.Microsecond).Add(24 * time.Hour)
	first := contextEvent(t, h, operator, start, "Europe/Minsk")
	second := contextEvent(t, h, operator, start.Add(time.Hour), "Europe/Moscow")
	contextPass(t, h, operator, user, first)
	contextPass(t, h, operator, user, second)
	taskID := h.submitText(t, 99, "context owned run")
	h.await(t, taskID, "completed")
	task, err := h.store.GetTask(context.Background(), taskID)
	require.NoError(t, err)
	require.Equal(t, userID, task.UserID)
	runA, runB := task.RunID, "different-run-"+uuid.NewString()
	bodyA := chooseContextBody("choose-a", first, 0, runA)
	code, data := adminRequest(t, h, user, http.MethodPost, "/internal/agent/context/choose", bodyA)
	require.Equal(t, http.StatusOK, code, string(data))
	original := contextResult(t, data)
	require.EqualValues(t, 1, original.SelectionVersion)
	code, data = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/choose",
		chooseContextBody("choose-b", second, 1, runB),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.EqualValues(t, 2, contextResult(t, data).SelectionVersion)
	h.stop(t)
	h.start(t)
	oldRun := resolveContext(t, h, user, events.ContextRequest{Module: "orders"}, runA)
	require.Equal(t, "pinned", oldRun.Source)
	require.Equal(t, first.ID, oldRun.Selected.ID)
	require.EqualValues(t, 2, oldRun.SelectionVersion)
	current := resolveContext(t, h, user, events.ContextRequest{Module: "orders"}, "")
	require.Equal(t, second.ID, current.Selected.ID)
	code, data = adminRequest(t, h, user, http.MethodPost, "/internal/agent/context/choose", bodyA)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, original, contextResult(t, data))
	code, data = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/status",
		map[string]any{"protocol_version": 1, "operation_id": "choose-a"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Equal(t, original, contextResult(t, data))
	code, data = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/choose",
		chooseContextBody("choose-a", second, 0, runA),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	code, data = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/choose",
		chooseContextBody("stale", first, 0, runA),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
	// An unavailable run pin falls back to the eligible user pin before a contextual hint.
	first.Document.Modules = []string{"registrations"}
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+first.ID.String(),
		map[string]any{
			"operation_id":     "disable-run-orders",
			"expected_version": first.Version,
			"document":         first.Document,
		},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	fallback := resolveContext(t, h, user, events.ContextRequest{Module: "orders", RequireExplicit: true}, runA)
	require.Equal(t, "pinned", fallback.Source)
	require.Equal(t, second.ID, fallback.Selected.ID)
	require.EqualValues(t, 2, fallback.SelectionVersion)
	other := resolveContext(t, h, otherUser, events.ContextRequest{Module: "orders"}, runA)
	require.Equal(t, "none", other.Source)
	require.Nil(t, other.Selected)
	code, data = adminRequest(
		t,
		h,
		otherUser,
		http.MethodPost,
		"/internal/agent/context/status",
		map[string]any{"protocol_version": 1, "operation_id": "choose-a"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
	require.NotContains(t, string(data), first.ID.String())
	_, err = h.store.SetUserRevoked(context.Background(), userID, "fixture")
	require.NoError(t, err)
	code, _ = adminRequest(t, h, user, http.MethodPost, "/internal/agent/context/choose", bodyA)
	require.Equal(t, http.StatusForbidden, code)
}

func TestContextIgnoresDisabledModulesAndNeverChoosesAmongCurrentEvents(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9921, true)
	_, user := registrationUser(t, h, 9922, false)
	now := time.Now().UTC().Truncate(time.Microsecond)
	first := contextEvent(t, h, operator, now.Add(-time.Hour), "Europe/Minsk")
	second := contextEvent(t, h, operator, now.Add(-30*time.Minute), "Europe/Moscow")
	contextPass(t, h, operator, user, first)
	contextPass(t, h, operator, user, second)
	result := resolveContext(t, h, user, events.ContextRequest{Module: "orders"}, "")
	require.Equal(t, "clarify", result.Source)
	require.Len(t, result.Choices, 2)
	first.Document.Modules = []string{"registrations"}
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		"/internal/admin/events/"+first.ID.String(),
		map[string]any{"operation_id": "disable-orders", "expected_version": first.Version, "document": first.Document},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	result = resolveContext(t, h, user, events.ContextRequest{Module: "orders", HintID: &first.ID}, "")
	require.Equal(t, "current", result.Source)
	require.Equal(t, second.ID, result.Selected.ID)
	code, data = adminRequest(
		t,
		h,
		user,
		http.MethodPost,
		"/internal/agent/context/choose",
		chooseContextBody("disabled", first, 0, uuid.NewString()),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "rejected")
}
