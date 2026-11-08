package api_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestRevokedIntakeAcknowledgesWithoutTaskOrRetry(t *testing.T) {
	t.Parallel()
	a := &intake{failure: db.ErrUserRevoked}
	w := post(newAPI(t, a), validUpdate, "test-secret")
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"ignored":true,"reason":"access_revoked"}`, w.Body.String())
	require.Equal(t, 1, a.calls)
}
