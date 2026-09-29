package integration_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserStartRequiresOneBoundedJSONValue(t *testing.T) {
	t.Parallel()
	f, service := browserFixture(t)
	for _, body := range []string{
		`{"username":"alice"}` + strings.Repeat(" ", 1024),
		`{"username":"alice"} {}`,
		`{"username":"alice"} trailing`,
	} {
		response := browserRequest(t, service.Handler(), http.MethodPost,
			"/miniapp/auth/start", browserOrigin, body)
		assert.Equal(t, http.StatusBadRequest, response.Code)
	}
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.browser_auth`).Scan(&count))
	assert.Zero(t, count, "invalid input must not create consent requests")
}

func TestBrowserCancelRequiresMatchingSecret(t *testing.T) {
	t.Parallel()
	f, service := browserFixture(t)
	id, cookie := startBrowser(t, service)
	foreign := *cookie
	foreign.Value = strings.Repeat("x", 32)
	path := "/miniapp/auth/cancel?request=" + id
	response := browserRequest(t, service.Handler(), http.MethodPost, path, browserOrigin, "", &foreign)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	var state string
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT state FROM bot.browser_auth WHERE id=$1`, id).Scan(&state))
	assert.Equal(t, "pending", state)
	for range 2 {
		response = browserRequest(t, service.Handler(), http.MethodPost, path, browserOrigin, "", cookie)
		assert.Equal(t, http.StatusOK, response.Code)
		assert.JSONEq(t, `{"result":"cancelled"}`, response.Body.String())
	}
}
