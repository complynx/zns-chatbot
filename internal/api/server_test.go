package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/task"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

type intake struct {
	calls     int
	failure   error
	duplicate bool
	id        uuid.UUID
}

func (a *intake) Accept(_ context.Context, _ telegram.Update, _ json.RawMessage) (db.Task, bool, error) {
	a.calls++
	return db.Task{ID: a.id}, a.duplicate, a.failure
}
func newAPI(t *testing.T, a *intake) *api.Server {
	t.Helper()
	s, err := api.New(
		&db.Store{},
		&identity.Client{},
		a,
		api.Options{
			BotID:          42,
			WebhookSecret:  "test-secret",
			OrganizationID: "org",
			ActorID:        "actor",
			Issuer:         "https://issuer.invalid",
			LoomAudience:   "loom",
		},
	)
	require.NoError(t, err)
	return s
}

const validUpdate = `{"update_id":1,"message":{"message_id":2,"from":{"id":123,"first_name":"Daniel"},"chat":{"id":123,"type":"private"},"text":"Hello"}}`

func TestPermanentlyUnsupportedIntakeDoesNotRequestRedelivery(t *testing.T) {
	t.Parallel()
	a := &intake{failure: task.ErrUnsupportedUpdate}
	s := newAPI(t, a)
	callback := `{"update_id":3,"callback_query":{"id":"callback-3","from":{"id":123,"first_name":"Daniel"},"message":{"message_id":2,"chat":{"id":123,"type":"private"}},"data":"invalid"}}`
	w := post(s, callback, "test-secret")
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"ignored":true`)
	require.NotContains(t, w.Body.String(), "task_id")
	require.Equal(t, 1, a.calls)
}

func post(s *api.Server, body, secret string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/telegram/webhook", strings.NewReader(body))
	r.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
	w := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(w, r)
	return w
}
func TestIntakeAcknowledgesOnlyCommittedTask(t *testing.T) {
	t.Parallel()
	a := &intake{id: uuid.New()}
	s := newAPI(t, a)
	w := post(s, validUpdate, "test-secret")
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Contains(t, w.Body.String(), a.id.String())
	require.Equal(t, 1, a.calls)
	a.failure = errors.New("transaction failed")
	w = post(s, validUpdate, "test-secret")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	a.failure = nil
	a.duplicate = true
	w = post(s, validUpdate, "test-secret")
	require.Equal(t, http.StatusAccepted, w.Code)
	require.Contains(t, w.Body.String(), `"duplicate":true`)
	s.SetAccepting(false)
	w = post(s, validUpdate, "test-secret")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, 3, a.calls)
}
func TestUnauthorizedAndInvalidUpdatesNeverReachQueue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, body, secret string
		status             int
	}{
		{"secret", validUpdate, "wrong", http.StatusUnauthorized},
		{"json", "{} garbage", "test-secret", http.StatusBadRequest},
		{
			"group",
			strings.ReplaceAll(validUpdate, `"type":"private"`, `"type":"group"`),
			"test-secret",
			http.StatusBadRequest,
		},
		{
			"foreign_chat",
			strings.ReplaceAll(validUpdate, `"chat":{"id":123`, `"chat":{"id":124`),
			"test-secret",
			http.StatusBadRequest,
		},
		{
			"bot",
			strings.ReplaceAll(validUpdate, `"first_name":"Daniel"`, `"first_name":"Bot","is_bot":true`),
			"test-secret",
			http.StatusBadRequest,
		},
		{"oversize", strings.Repeat(" ", 1<<20+1), "test-secret", http.StatusRequestEntityTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := &intake{}
			s := newAPI(t, a)
			require.Equal(t, tc.status, post(s, tc.body, tc.secret).Code)
			require.Zero(t, a.calls)
		})
	}
}
func TestManifestAndHealthAreAvailableWithoutIAM(t *testing.T) {
	t.Parallel()
	s := newAPI(t, &intake{})
	w := httptest.NewRecorder()
	s.PublicHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusOK, w.Code)
	w = httptest.NewRecorder()
	s.InternalHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/agent/manifest", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var manifest map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &manifest))
	require.InDelta(t, float64(1), manifest["protocol_version"], 0)
	require.Equal(t, "zinusya", manifest["service_id"])
	require.Contains(t, w.Body.String(), "Zinusya")
	require.Contains(t, w.Body.String(), "zinusya_account")
	require.Contains(t, w.Body.String(), "zinusya_administration")
	require.NotContains(t, w.Body.String(), "test-secret")
	w = httptest.NewRecorder()
	r := httptest.NewRequest(
		http.MethodPost,
		"/internal/agent/account/me",
		strings.NewReader(`{"protocol_version":1,"operation_id":"op","arguments":{}}`),
	)
	r.Header.Set("Content-Type", "application/json")
	s.InternalHandler().ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAccountToolForwardsBearerToIdentityVerification(t *testing.T) {
	t.Parallel()
	s := newAPI(t, &intake{})
	r := httptest.NewRequest(
		http.MethodPost,
		"/internal/agent/account/me",
		strings.NewReader(`{"protocol_version":1,"operation_id":"op","arguments":{}}`),
	)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer invalid-jwt")
	w := httptest.NewRecorder()
	s.InternalHandler().ServeHTTP(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "invalid identity")
}

func TestFileToolsUseConsumerReadableStaticManifest(t *testing.T) {
	t.Parallel()
	server := newAPI(t, &intake{})
	output := httptest.NewRecorder()
	server.InternalHandler().ServeHTTP(output, httptest.NewRequest(http.MethodGet, "/internal/agent/manifest", nil))
	var manifest struct {
		Modules []struct {
			ID        string `json:"id"`
			Discovery string `json:"discovery"`
			Methods   []struct {
				ID string `json:"id"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(output.Body.Bytes(), &manifest))
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_files" {
			continue
		}
		require.Equal(t, "static", module.Discovery)
		names := []string{}
		for _, method := range module.Methods {
			names = append(names, method.ID)
		}
		require.ElementsMatch(t, []string{"info", "fetch"}, names)
		return
	}
	t.Fatal("file tools not discoverable")
}
