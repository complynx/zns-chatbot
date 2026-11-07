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
	"github.com/complynx/zns-chatbot/internal/events"
)

func TestEventCatalogIsScopedDurableAndIdempotent(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	task := unknownTask(t, h)
	operator := taskAdminToken(t, h, task)
	id := uuid.New()
	path := "/internal/admin/events/" + id.String()
	start := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	doc := events.Document{
		Name:     "Grodno",
		Timezone: "Europe/Minsk",
		StartsAt: start,
		EndsAt:   start.Add(48 * time.Hour),
		Modules:  []string{"orders", "massage"},
	}
	body := map[string]any{"operation_id": "create-event", "expected_version": 0, "document": doc}
	code, data := adminRequest(t, h, operator, http.MethodPut, path, body)
	require.Equal(t, http.StatusOK, code, string(data))
	var created events.Event
	require.NoError(t, json.Unmarshal(data, &created))
	require.Equal(t, id, created.ID)
	require.EqualValues(t, 1, created.Version)
	require.Equal(t, []string{"massage", "orders"}, created.Document.Modules)
	code, data = adminRequest(t, h, operator, http.MethodPut, path, body)
	require.Equal(t, http.StatusOK, code, string(data))
	var replay events.Event
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, created, replay)
	doc.Name = "Changed"
	doc.Timezone = "Asia/Yekaterinburg"
	doc.Modules = []string{"massage"}
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		path,
		map[string]any{"operation_id": "update-event", "expected_version": 1, "document": doc},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var changed events.Event
	require.NoError(t, json.Unmarshal(data, &changed))
	require.EqualValues(t, 2, changed.Version)
	code, _ = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		path,
		map[string]any{"operation_id": "stale-event", "expected_version": 1, "document": doc},
	)
	require.Equal(t, http.StatusConflict, code)
	code, _ = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		path,
		map[string]any{"operation_id": "create-event", "expected_version": 0, "document": doc},
	)
	require.Equal(t, http.StatusConflict, code)
	code, data = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/event-operations/create-event", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, created, replay)
	h.stop(t)
	h.start(t)
	code, data = adminRequest(t, h, operator, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, changed, replay)
	code, data = adminRequest(t, h, operator, http.MethodPut, path, body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, created, replay)
	code, data = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/events?limit=1", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), id.String())
	code, data = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/events?after="+id.String(), nil)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NotContains(t, string(data), id.String())
	foreign, err := h.store.ChangeEvent(
		context.Background(),
		db.EventScope{BotID: 999, Organization: "org"},
		"operator",
		"foreign-event",
		db.EventChange{ID: uuid.New(), Document: doc},
	)
	require.NoError(t, err)
	code, _ = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/events/"+foreign.ID.String(), nil)
	require.Equal(t, http.StatusNotFound, code)
	code, data = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/events", nil)
	require.Equal(t, http.StatusOK, code)
	require.NotContains(t, string(data), foreign.ID.String())
}

func TestEventCatalogRejectsInvalidOrUnauthorizedChanges(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	id := unknownTask(t, h)
	task, err := h.store.GetTask(context.Background(), id)
	require.NoError(t, err)
	user, err := h.store.GetUser(context.Background(), task.UserID)
	require.NoError(t, err)
	basic := p.token(user.Subject)
	start := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	doc := events.Document{
		Name:     "Event",
		Timezone: "Europe/Moscow",
		StartsAt: start,
		EndsAt:   start.Add(time.Hour),
		Modules:  []string{},
	}
	path := "/internal/admin/events/" + uuid.NewString()
	body := map[string]any{"operation_id": "denied", "expected_version": 0, "document": doc}
	code, _ := adminRequest(t, h, basic, http.MethodPut, path, body)
	require.Equal(t, http.StatusForbidden, code)
	operator := taskAdminToken(t, h, id)
	doc.Timezone = "Local"
	body["document"] = doc
	code, _ = adminRequest(t, h, operator, http.MethodPut, path, body)
	require.Equal(t, http.StatusBadRequest, code)
	doc.Timezone = "Europe/Moscow"
	doc.EndsAt = doc.StartsAt
	body["document"] = doc
	code, _ = adminRequest(t, h, operator, http.MethodPut, path, body)
	require.Equal(t, http.StatusBadRequest, code)
	doc.EndsAt = doc.StartsAt.Add(time.Nanosecond)
	body["document"] = doc
	code, _ = adminRequest(t, h, operator, http.MethodPut, path, body)
	require.Equal(t, http.StatusBadRequest, code)
	body["document"] = map[string]any{
		"name":      "Naive",
		"timezone":  "Europe/Moscow",
		"starts_at": "2026-11-10T12:00:00",
		"ends_at":   "2026-11-10T13:00:00",
		"modules":   []string{},
	}
	code, _ = adminRequest(t, h, operator, http.MethodPut, path, body)
	require.Equal(t, http.StatusUnprocessableEntity, code)
	code, _ = adminRequest(t, h, operator, http.MethodGet, path, nil)
	require.Equal(t, http.StatusNotFound, code)
	code, _ = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/event-operations/denied", nil)
	require.Equal(t, http.StatusNotFound, code)
	_, err = h.store.SetUserRevoked(context.Background(), user.ID, "operator")
	require.NoError(t, err)
	code, _ = adminRequest(t, h, operator, http.MethodGet, "/internal/admin/events", nil)
	require.Equal(t, http.StatusForbidden, code)
}

func TestEventConcurrentRetriesAndEditsApplyOnce(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	scope := db.EventScope{BotID: 123, Organization: "org"}
	id := uuid.New()
	start := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	document := events.Document{
		Name:     "Concurrent",
		Timezone: "Europe/Minsk",
		StartsAt: start,
		EndsAt:   start.Add(time.Hour),
		Modules:  []string{"orders"},
	}
	type outcome struct {
		event     events.Event
		err       error
		operation string
	}
	results := make(chan outcome, 6)
	for range 6 {
		go func() {
			result, err := h.store.ChangeEvent(
				context.Background(),
				scope,
				"operator",
				"same-create",
				db.EventChange{ID: id, Document: document},
			)
			results <- outcome{event: result, err: err}
		}()
	}
	for range 6 {
		result := <-results
		require.NoError(t, result.err)
		require.EqualValues(t, 1, result.event.Version)
		require.Equal(t, id, result.event.ID)
	}
	for _, name := range []string{"left", "right"} {
		go func() {
			copyDocument := document
			copyDocument.Name = name
			result, err := h.store.ChangeEvent(
				context.Background(),
				scope,
				"operator",
				name,
				db.EventChange{ID: id, ExpectedVersion: 1, Document: copyDocument},
			)
			results <- outcome{event: result, err: err, operation: name}
		}()
	}
	successful := 0
	for range 2 {
		result := <-results
		if result.err != nil {
			require.ErrorIs(t, result.err, db.ErrConflict)
			continue
		}
		successful++
		require.EqualValues(t, 2, result.event.Version)
		receipt, err := h.store.EventOperation(context.Background(), scope, "operator", result.operation)
		require.NoError(t, err)
		require.Equal(t, result.event, receipt)
	}
	require.Equal(t, 1, successful)
	current, err := h.store.Event(context.Background(), scope, id)
	require.NoError(t, err)
	require.EqualValues(t, 2, current.Version)
	list, err := h.store.Events(context.Background(), scope, uuid.Nil, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
}
