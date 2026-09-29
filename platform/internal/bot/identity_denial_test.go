package bot_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestOnboardingClassifiesOnlyExactPermanentConflict(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, code string
		status     int
		denied     bool
	}{
		{"identity-conflict", "identity_conflict", http.StatusConflict, true},
		{"other-conflict", "other_conflict", http.StatusConflict, false},
		{"invalid-protocol", "invalid_json", http.StatusBadRequest, false},
		{"service-auth", "unauthorized", http.StatusUnauthorized, false},
		{"unavailable", "identity_provisioning_unavailable", http.StatusServiceUnavailable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": test.code})
			}))
			t.Cleanup(server.Close)
			err := (bot.APIClient{Base: server.URL}).ProvisionTelegram(t.Context(), 77, telegram.User{ID: 101})
			if test.denied {
				require.ErrorIs(t, err, bot.ErrProvisioningDenied)
			} else {
				require.Error(t, err)
				require.NotErrorIs(t, err, bot.ErrProvisioningDenied)
			}
		})
	}
}
