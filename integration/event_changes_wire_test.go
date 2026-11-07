package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestAgentEventChangeAcceptsAdvertisedNullPoliciesAndOptionalTierFlags(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	for _, test := range []string{"remove-policies", "null-pricing", "omitted-tier-flags"} {
		t.Run(test, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(args)
			require.NoError(t, err)
			var arguments map[string]any
			require.NoError(t, json.Unmarshal(raw, &arguments))
			document := arguments["document"].(map[string]any)
			if test == "remove-policies" {
				document["registration"], document["deposit"] = nil, nil
			} else {
				rules := document["registration"].(map[string]any)
				rules["pricing"] = nil
				if test == "omitted-tier-flags" {
					rules["pricing"] = map[string]any{"mode": "paired", "tiers": []any{map[string]any{
						"id": "standard", "amount": 10, "price_minor": 12345, "starts_at": args.Document.StartsAt,
					}}}
				}
			}
			code, response := adminRequest(t, h, operator, http.MethodPost, eventChangeAPI+"/prepare", map[string]any{
				"protocol_version": 1, "operation_id": test, "arguments": arguments,
			})
			require.Equal(t, http.StatusOK, code, string(response))
			require.Contains(t, string(response), `"status":"prepared"`)
			var normalized struct {
				Arguments db.EventChange `json:"arguments"`
			}
			require.NoError(t, json.Unmarshal(response, &normalized))
			switch test {
			case "remove-policies":
				require.Nil(t, normalized.Arguments.Document.Registration)
				require.Nil(t, normalized.Arguments.Document.Deposit)
			case "null-pricing":
				require.Nil(t, normalized.Arguments.Document.Registration.Pricing)
			case "omitted-tier-flags":
				tiers := normalized.Arguments.Document.Registration.Pricing.Tiers
				require.Len(t, tiers, 1)
				require.False(t, tiers[0].Promo)
				require.False(t, tiers[0].BlockedByDate)
			}
		})
	}
}

func TestAgentEventChangeRejectsUnknownConfigurationFieldsAndNaiveInstants(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	for _, test := range []string{"unknown-document", "unknown-registration", "unknown-tier", "naive-instant"} {
		t.Run(test, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(args)
			require.NoError(t, err)
			var arguments map[string]any
			require.NoError(t, json.Unmarshal(raw, &arguments))
			document := arguments["document"].(map[string]any)
			switch test {
			case "unknown-document":
				document["surprise"] = true
			case "unknown-registration":
				document["registration"].(map[string]any)["surprise"] = true
			case "unknown-tier":
				document["registration"].(map[string]any)["pricing"] = map[string]any{
					"mode": "paired", "tiers": []any{map[string]any{
						"id": "standard", "amount": 10, "price_minor": 12345,
						"starts_at": args.Document.StartsAt, "surprise": true,
					}},
				}
			case "naive-instant":
				document["starts_at"] = "2026-11-10T12:00:00"
			}
			code, response := adminRequest(t, h, operator, http.MethodPost, eventChangeAPI+"/prepare", map[string]any{
				"protocol_version": 1, "operation_id": test, "arguments": arguments,
			})
			require.GreaterOrEqual(t, code, http.StatusBadRequest, string(response))
			require.Less(t, code, http.StatusInternalServerError, string(response))
		})
	}
}
