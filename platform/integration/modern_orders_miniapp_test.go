package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/miniapp"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestModernMiniAppFullChoiceGateway(t *testing.T) {
	t.Parallel()
	for _, stem := range []string{"a", "Ж", `<&\"`} {
		t.Run(fmt.Sprintf("%x", stem), func(t *testing.T) {
			t.Parallel()
			f, original := modernSingleKeyOrder(t, stem)
			handler := (miniapp.Gateway{API: f.b.API, Token: "test-token", EventID: original.EventID}).Handler()
			choice := orders.ChoiceInput{Extras: map[string]json.RawMessage{}}
			for key := range original.Choice.Extras {
				choice.Extras[key] = json.RawMessage(`0`)
			}
			// The browser assembles the customer from name fields. Keep its own canonical result.
			choice.FirstName = "Alice"
			quoted := webRequest(t, handler, http.MethodPost, "/miniapp/api/quote", 101, choice)
			assert.Equal(t, http.StatusOK, quoted.Code, quoted.Body.String())
			path := "/miniapp/api/orders/" + original.ID
			body := map[string]any{"version": original.Version, "key": "full-miniapp", "choice": choice}
			saved := webRequest(t, handler, http.MethodPost, path, 101, body)
			require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
			var updated orders.Order
			require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &updated))
			var canonical orders.Choice
			require.NoError(t, json.Unmarshal(quoted.Body.Bytes(), &canonical))
			assert.Equal(t, canonical, updated.Choice)
			assert.Equal(t, original.Choice.Extras, updated.Choice.Extras)
			assert.JSONEq(
				t,
				saved.Body.String(),
				webRequest(t, handler, http.MethodPost, path, 101, body).Body.String(),
			)
			body["key"] = "stale-miniapp"
			assert.Equal(t, http.StatusConflict, webRequest(t, handler, http.MethodPost, path, 101, body).Code)
			assert.Equal(t, http.StatusNotFound, webRequest(t, handler, http.MethodPost, path, 202, body).Code)
			assert.Equal(t, http.StatusForbidden, webRequest(t, handler, http.MethodPost, path, 303, body).Code)
			choice.FirstName = strings.Repeat("x", 321<<10)
			assert.Equal(
				t,
				http.StatusBadRequest,
				webRequest(t, handler, http.MethodPost, "/miniapp/api/quote", 101, choice).Code,
			)
			body["choice"] = choice
			assert.Equal(t, http.StatusBadRequest, webRequest(t, handler, http.MethodPost, path, 101, body).Code)
		})
	}
}
