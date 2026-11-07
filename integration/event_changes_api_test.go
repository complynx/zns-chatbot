package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

const eventChangeAPI = "/internal/agent/administration/change_event"

func prepareEventBody(t *testing.T, h *harness, token, op string, args db.EventChange) (map[string]any, int) {
	t.Helper()
	code, raw := adminRequest(t, h, token, http.MethodPost, eventChangeAPI+"/prepare", map[string]any{
		"protocol_version": 1, "operation_id": op, "arguments": args,
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	var prep struct {
		Status        string         `json:"status"`
		Arguments     db.EventChange `json:"arguments"`
		Preconditions map[string]any `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.Equal(t, "prepared", prep.Status)
	return map[string]any{
		"protocol_version": 1, "operation_id": op, "idempotency_key": op, "arguments": prep.Arguments,
		"confirmation": map[string]any{"mode": "exact_parameters", "preconditions": prep.Preconditions},
	}, len(raw)
}

func eventResult(t *testing.T, raw []byte) events.Event {
	t.Helper()
	var result struct {
		Status string       `json:"status"`
		Result events.Event `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status, string(raw))
	return result.Result
}

func TestAgentEventChangeRequiresExactApprovalAndRecoversOriginalResult(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	h.start(t)
	h.stop(t)
	basic := h.peers.token(scope.Subject)
	code, raw := adminRequest(t, h, basic, http.MethodPost, eventChangeAPI+"/prepare", map[string]any{
		"protocol_version": 1, "operation_id": "event-api", "arguments": args,
	})
	require.Equal(t, http.StatusForbidden, code, string(raw))
	operator := settingsAdminToken(h, scope)
	body, _ := prepareEventBody(t, h, operator, "event-api", args)
	confirmation := body["confirmation"]
	delete(body, "confirmation")
	code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI, body)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	_, err := h.store.Event(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, args.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	body["confirmation"] = confirmation
	code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	first := eventResult(t, raw)
	require.EqualValues(t, 1, first.Version)
	update := db.EventChange{ID: args.ID, ExpectedVersion: first.Version, Document: first.Document}
	update.Document.Name = "Later operator version"
	_, err = h.store.ChangeEvent(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		scope.Subject,
		"later-event",
		update,
	)
	require.NoError(t, err)
	h.start(t)
	h.stop(t)
	for _, suffix := range []string{"status", "reconcile"} {
		code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI+"/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": "event-api"})
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Equal(t, first, eventResult(t, raw))
	}
	code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, first, eventResult(t, raw))
	_, err = h.store.SetUserRevoked(t.Context(), scope.UserID, "fixture")
	require.NoError(t, err)
	for _, suffix := range []string{"", "/status", "/reconcile"} {
		request := body
		if suffix != "" {
			request = map[string]any{"protocol_version": 1, "operation_id": "event-api"}
		}
		code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI+suffix, request)
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
}

func TestAgentEventChangeCarriesFullConfigurationAndAdvertisedSchema(t *testing.T) {
	t.Parallel()
	h, scope, args := eventChangeFixture(t)
	args.Document.Name = strings.Repeat("Я", 256)
	args.Document.Modules = nil
	policy := &registrations.TierPolicy{Mode: registrations.PricingPaired}
	for i := range 100 {
		args.Document.Modules = append(args.Document.Modules, fmt.Sprintf("m%03d", i)+strings.Repeat("x", 124))
		policy.Tiers = append(policy.Tiers, registrations.Tier{
			ID: fmt.Sprintf(
				"%03d",
				i,
			) + strings.Repeat(
				"x",
				61,
			),
			Amount:     1,
			PriceMinor: 12345,
			StartsAt:   args.Document.StartsAt,
		})
	}
	args.Document.Registration.Pricing = policy
	before, err := h.store.ChangeEvent(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		scope.Subject,
		"large-base",
		args,
	)
	require.NoError(t, err)
	args.ExpectedVersion = before.Version
	h.start(t)
	h.stop(t)
	operator := settingsAdminToken(h, scope)
	code, raw := agentAdminRead(t, h, operator, "discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var discovery struct {
		Methods []struct {
			ID        string      `json:"id"`
			Input     huma.Schema `json:"input_schema"`
			MaxResult int         `json:"max_result_bytes"`
		} `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(raw, &discovery))
	var input *huma.Schema
	for i := range discovery.Methods {
		if discovery.Methods[i].ID == "change_event" {
			input = &discovery.Methods[i].Input
			require.Equal(t, 256<<10, discovery.Methods[i].MaxResult)
		}
	}
	require.NotNil(t, input)
	encoded, err := json.Marshal(args)
	require.NoError(t, err)
	var value any
	require.NoError(t, json.Unmarshal(encoded, &value))
	input.PrecomputeMessages()
	validation := &huma.ValidateResult{}
	huma.Validate(
		huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer),
		input,
		huma.NewPathBuffer(nil, 0),
		huma.ModeWriteToServer,
		value,
		validation,
	)
	require.Empty(t, validation.Errors)
	body, preparationBytes := prepareEventBody(t, h, operator, "large-event", args)
	request, err := json.Marshal(body)
	require.NoError(t, err)
	require.Greater(t, len(request), 8192)
	require.Less(t, len(request), 128<<10)
	require.Greater(t, preparationBytes, 16384)
	require.Less(t, preparationBytes, 256<<10)
	t.Logf("Full event confirmation=%d bytes; preparation=%d bytes", len(request), preparationBytes)
	code, raw = adminRequest(t, h, operator, http.MethodPost, eventChangeAPI, body)
	require.Equal(t, http.StatusOK, code, string(raw))
	result := eventResult(t, raw)
	require.Len(t, result.Document.Modules, 100)
	require.Equal(t, policy, result.Document.Registration.Pricing)
}
