package integration_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestPassProfileHTTPContract(t *testing.T) {
	t.Parallel()
	db := database(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	server := httptest.NewServer(
		api.Handler(runtimeapp.NewServices(db, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler)),
	)
	t.Cleanup(server.Close)
	client := bot.APIClient{Base: server.URL, Signer: signer, HTTP: server.Client()}
	alice, err := client.PassProfile(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "alice", alice.Owner)
	assert.Zero(t, alice.Version)
	command := profileCommand("set", "legal_name", "name", alice)
	command.Origin = "agent"
	command.Value = "TEST Name"
	alice, err = client.ExecutePassProfile(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, command.Value, alice.LegalName)
	replay, err := client.ExecutePassProfile(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, alice, replay)
	command.Value = "Changed Replay"
	_, err = client.ExecutePassProfile(t.Context(), "alice", command)
	requireCode(t, err, "idempotency_conflict")
	command.Key = "stale"
	_, err = client.ExecutePassProfile(t.Context(), "alice", command)
	requireCode(t, err, "pass_profile_stale")
	bob, err := client.PassProfile(t.Context(), "bob")
	require.NoError(t, err)
	assert.Empty(t, bob.LegalName)
	for _, origin := range []string{"manual", "agent"} {
		command = profileCommand("set", "passport", "passport-"+origin, alice)
		command.Value, command.Origin = "TEST ONLY "+origin, origin
		alice, err = client.ExecutePassProfile(t.Context(), "alice", command)
		require.NoError(t, err)
		assert.Equal(t, command.Value, alice.Passport)
		command.Version = 0
		_, err = client.ExecutePassProfile(t.Context(), "visitor", command)
		requireCode(t, err, "forbidden")
	}
	_, err = client.PassProfile(t.Context(), "unknown")
	requireCode(t, err, "forbidden")
	unchanged, err := client.PassProfile(t.Context(), "bob")
	require.NoError(t, err)
	assert.Equal(t, bob, unchanged)
	history, err := client.PassProfileHistory(t.Context(), "alice")
	require.NoError(t, err)
	require.Len(t, history, 3)
	assert.Equal(t, "legal_name", history[0].Field)
	assert.Equal(t, "manual", history[1].Origin)
	assert.Equal(t, "agent", history[2].Origin)
	assert.Equal(t, alice.Version, history[2].Version)
	bobHistory, err := client.PassProfileHistory(t.Context(), "bob")
	require.NoError(t, err)
	assert.Empty(t, bobHistory)
	_, err = client.PassProfileHistory(t.Context(), "unknown")
	requireCode(t, err, "forbidden")
}

func TestPassProfileHTTPStrictAuthenticationAndPrivacy(t *testing.T) {
	t.Parallel()
	db := database(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("k", 32))}
	var logs bytes.Buffer
	handler := api.Handler(
		runtimeapp.NewServices(db, runtimeapp.Options{}),
		signer,
		slog.New(slog.NewTextHandler(&logs, nil)),
	)
	for _, body := range []string{
		`{"owner":"bob"}`, `{"name":"set","field":"legal_name","value":"PRIVATE VALUE","version":0,"key":"test","origin":"manual","extra":true}`,
		`{} {}`, strings.Repeat(" ", 65537), `{"name":`,
	} {
		request := httptest.NewRequest(http.MethodPost, "/v1/me/pass-profile/actions", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+signer.Token("alice"))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assert.Equal(t, http.StatusBadRequest, response.Code)
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		assert.NotContains(t, response.Body.String(), "PRIVATE VALUE")
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/v1/me/pass-profile"
		if method == http.MethodPost {
			path += "/actions"
		}
		request := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		assert.Equal(t, http.StatusUnauthorized, response.Code)
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	}
	service := passes.Service{DB: db}
	_, err := service.Execute(
		t.Context(),
		"alice",
		passes.Command{Name: "set", Field: "passport", Value: "PRIVATE PASSPORT", Key: "seed", Origin: "manual"},
	)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodGet, "/v1/me/pass-profile?owner=alice", nil)
	request.Header.Set("Authorization", "Bearer "+signer.Token("bob"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	assert.NotContains(t, response.Body.String(), "PRIVATE PASSPORT")
	assert.NotContains(t, logs.String(), "PRIVATE")
	_, err = db.Exec(t.Context(), `CREATE FUNCTION core.reject_profile_test() RETURNS trigger LANGUAGE plpgsql AS $$
	BEGIN RAISE EXCEPTION 'PRIVATE DATABASE ERROR %', NEW.passport; END $$;
	CREATE TRIGGER reject_profile_test BEFORE UPDATE ON core.pass_profiles
	FOR EACH ROW EXECUTE FUNCTION core.reject_profile_test()`)
	require.NoError(t, err)
	request = httptest.NewRequest(http.MethodPost, "/v1/me/pass-profile/actions", strings.NewReader(
		`{"name":"set","field":"passport","value":"PRIVATE REPLACEMENT","version":1,"key":"rejected","origin":"agent"}`,
	))
	request.Header.Set("Authorization", "Bearer "+signer.Token("alice"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.NotContains(t, response.Body.String(), "PRIVATE")
	assert.NotContains(t, logs.String(), "PRIVATE")
	assert.Contains(t, logs.String(), "Pass profile request failed")
}
