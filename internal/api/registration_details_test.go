package api_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/api"
	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func TestRegistrationDetailsValidationDoesNotEchoSensitiveInput(t *testing.T) {
	t.Parallel()
	protector, err := registrationdetails.NewProtector(bytes.Repeat([]byte{75}, 32))
	require.NoError(t, err)
	server, err := api.New(&db.Store{}, &identity.Client{}, &intake{}, api.Options{
		BotID: 42, WebhookSecret: "test-secret", OrganizationID: "org", ActorID: "actor",
		Issuer: "https://issuer.invalid", LoomAudience: "loom", RegistrationDetails: protector,
	})
	require.NoError(t, err)
	const marker = "SYNTHETIC-PRIVATE-CANARY"
	const envelope = `{"protocol_version":1,"operation_id":"11111111-1111-4111-8111-111111111111","idempotency_key":"11111111-1111-4111-8111-111111111111","confirmation":{"mode":"none"},"arguments":{"expected_version":0,`
	for _, testcase := range []struct{ name, method, tail string }{
		{"long passport", "set_passport_number", fmt.Sprintf(`"passport_number":%q}}`, strings.Repeat(marker, 8))},
		{"long legal name", "set_legal_name", fmt.Sprintf(`"legal_name":%q}}`, strings.Repeat(marker, 16))},
		{"wrong type", "set_passport_number", fmt.Sprintf(`"passport_number":[%q]}}`, marker)},
		{"malformed JSON", "set_passport_number", fmt.Sprintf(`"passport_number":%q`, marker)},
	} {
		t.Run(testcase.name, func(t *testing.T) {
			t.Parallel()
			body := envelope + testcase.tail
			request := httptest.NewRequest(
				http.MethodPost,
				"/internal/agent/registration_details/"+testcase.method,
				strings.NewReader(body),
			)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer synthetic-fixture")
			response := httptest.NewRecorder()
			server.InternalHandler().ServeHTTP(response, request)
			require.GreaterOrEqual(t, response.Code, 400)
			require.Less(t, response.Code, 500)
			require.NotContains(t, response.Body.String(), marker)
			require.NotContains(t, response.Body.String(), body)
		})
	}
}
