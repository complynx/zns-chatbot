package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestEventContextPricingMatchesAdvertisedOutput(t *testing.T) {
	t.Parallel()
	schemas := eventContextContractSchemas(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, mode := range []string{"fixed", "paired", "distributed"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			rules := &registrations.Rules{LeaderCapacity: 10, FollowerCapacity: 10, PriceMinor: 10000,
				Currency: "EUR", CurrencyScale: 2, ClosesAt: now.Add(time.Hour), PaymentWindowSeconds: 3600}
			if mode != "fixed" {
				rules.Pricing = &registrations.TierPolicy{Mode: mode, Tiers: []registrations.Tier{
					{ID: "early", Amount: 2, PriceMinor: 10000, StartsAt: now.Add(-time.Hour)},
					{ID: "regular", Amount: 10, PriceMinor: 20000, StartsAt: now.Add(time.Hour), BlockedByDate: true},
				}}
			}
			document, err := events.Normalize(
				events.Document{Name: "Contract event", Timezone: "Europe/Minsk", StartsAt: now.Add(time.Hour),
					EndsAt: now.Add(24 * time.Hour), Modules: []string{"registrations"}, Registration: rules},
			)
			require.NoError(t, err)
			event := events.Event{ID: uuid.New(), Version: 1, Document: document}
			resolved, err := events.ResolveContext(
				events.ContextRequest{Module: "registrations"},
				uuid.Nil,
				[]events.Event{event},
				now,
			)
			require.NoError(t, err)
			require.NotNil(t, resolved.Selected)
			if mode != "fixed" {
				require.NotNil(t, resolved.Selected.Registration.Pricing)
			}
			for _, method := range []string{"resolve", "list", "choose"} {
				t.Run(method, func(t *testing.T) {
					t.Parallel()
					var output any = resolved
					if method == "list" {
						output = events.ContextList{Events: []events.ContextEvent{events.ContextView(event)}}
					}
					encoded, encodeErr := json.Marshal(output)
					require.NoError(t, encodeErr)
					var value any
					require.NoError(t, json.Unmarshal(encoded, &value))
					validation := &huma.ValidateResult{}
					huma.Validate(huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer), schemas[method],
						huma.NewPathBuffer(nil, 0), huma.ModeReadFromServer, value, validation)
					require.Empty(t, validation.Errors, "event context must satisfy the contract consumed by Loom")
				})
			}
		})
	}
}

func eventContextContractSchemas(t *testing.T) map[string]*huma.Schema {
	t.Helper()
	response := httptest.NewRecorder()
	newAPI(
		t,
		&intake{},
	).InternalHandler().
		ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/agent/manifest", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID     string      `json:"id"`
				Output huma.Schema `json:"output_schema"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &manifest))
	schemas := map[string]*huma.Schema{}
	for _, module := range manifest.Modules {
		if module.ID == "zinusya_context" {
			for _, method := range module.Methods {
				method.Output.PrecomputeMessages()
				schemas[method.ID] = &method.Output
			}
		}
	}
	require.Len(t, schemas, 3)

	return schemas
}
