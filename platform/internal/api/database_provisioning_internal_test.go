package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/identityprovision"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type databaseProvisioner struct{ failure error }

func (p databaseProvisioner) EnsureTelegram(
	context.Context,
	identityprovision.Telegram,
) (identityprovision.Binding, error) {
	return identityprovision.Binding{}, p.failure
}

func TestProvisioningDatabaseHeaderRequiresSQLCause(t *testing.T) {
	t.Parallel()
	signer := identity.Signer{Key: []byte("01234567890123456789012345678901")}
	body, err := json.Marshal(identity.TelegramProvisioningRequest{BotID: 77, User: telegram.User{ID: 101}})
	require.NoError(t, err)
	for _, sample := range []struct {
		name    string
		failure error
		marked  bool
	}{
		{"sql", &pgconn.PgError{Code: "P0001", Message: "private SQL password=secret"}, true},
		{"provider", errors.New("provider unavailable"), false},
	} {
		t.Run(sample.name, func(t *testing.T) {
			t.Parallel()
			handler := WithTelegramProvisioning(http.NotFoundHandler(), databaseProvisioner{sample.failure}, signer, 77)
			request := httptest.NewRequest(http.MethodPost, "/internal/identity/telegram", bytes.NewReader(body))
			request.Header.Set("Authorization", "Bearer "+signer.TelegramProvisioningToken(77, body))
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			assert.JSONEq(t, `{"code":"identity_provisioning_unavailable"}`, recorder.Body.String())
			assert.Equal(t, sample.marked, recorder.Header().Get(core.DatabaseFailureHeader) == "1")
			assert.NotContains(t, recorder.Body.String(), "password=secret")
		})
	}
}
