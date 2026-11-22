package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func invitationAPIRequest(t *testing.T, f pairFixture, token, method, operation string, args any) (int, []byte) {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args}
	if method == "invite" || method == "accept" || method == "decline" || method == "revoke" {
		body["idempotency_key"] = operation
		body["confirmation"] = map[string]any{"mode": "none"}
	}
	return adminRequest(t, f.H, token, http.MethodPost, "/internal/agent/pairs/"+method, body)
}

func TestPairInvitationContextMatchesDiscoveredSchemaForBothPolicies(t *testing.T) {
	t.Parallel()
	f := setupPair(t)
	actor := pairUserToken(f, f.A)
	code, raw := adminRequest(t, f.H, actor, http.MethodPost, "/internal/agent/pairs/discover", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(raw))
	var discovery struct {
		Methods []struct {
			ID     string      `json:"id"`
			Output huma.Schema `json:"output_schema"`
		} `json:"methods"`
	}
	require.NoError(t, json.Unmarshal(raw, &discovery))
	var schema *huma.Schema
	for _, method := range discovery.Methods {
		if method.ID == "invitation_context" {
			schema = &method.Output
			break
		}
	}
	require.NotNil(t, schema)
	schema.PrecomputeMessages()
	read := func(eventID uuid.UUID) map[string]any {
		t.Helper()
		args := map[string]any{"event_id": eventID}
		status, data := invitationAPIRequest(t, f, actor, "invitation_context", uuid.NewString(), args)
		require.Equal(t, http.StatusOK, status, string(data))
		var response struct {
			Status string         `json:"status"`
			Result map[string]any `json:"result"`
		}
		require.NoError(t, json.Unmarshal(data, &response))
		require.Equal(t, "succeeded", response.Status)
		validation := &huma.ValidateResult{}
		huma.Validate(huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer), schema,
			huma.NewPathBuffer(nil, 0), huma.ModeReadFromServer, response.Result, validation)
		require.Empty(t, validation.Errors)
		return response.Result
	}
	directed := read(f.EventID)
	require.Equal(t, f.Entry.ID.String(), directed["registration_id"])
	require.NotContains(t, directed, "pass_command")

	adminID, _ := registrationUser(t, f.H, 12005, true)
	admin := paymentScope(t, f.H, adminID, true)
	event, err := f.H.store.Event(t.Context(), admin.EventScope, f.EventID)
	require.NoError(t, err)
	event.Document.Slug = "pair-sale"
	event.Document.Registration.AutoAssign = true
	sale, err := f.H.store.ChangeEvent(t.Context(), admin.EventScope, admin.Subject, "create-code-sale",
		db.EventChange{ID: uuid.New(), Document: event.Document})
	require.NoError(t, err)
	handoff := read(sale.ID)
	require.Equal(t, "/passes pair-sale --pair", handoff["pass_command"])
	for _, field := range []string{"registration_id", "expected_version", "role"} {
		require.NotContains(t, handoff, field)
	}
}

func TestPairInvitationAPIUsesOwnPartnerCodeAndRequiresTargetConsent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := setupPair(t)
	a, b, other := pairUserToken(f, f.A), pairUserToken(f, f.B), pairUserToken(f, f.Other)
	code, data := adminRequest(
		t,
		f.H,
		b,
		http.MethodPost,
		"/internal/agent/pairs/partner_code",
		map[string]any{"protocol_version": 1, "operation_id": "partner-code", "arguments": map[string]any{}},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var account struct {
		Result struct {
			ID       uuid.UUID `json:"partner_code"`
			ShareURL string    `json:"share_url"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &account))
	require.Equal(t, f.B.UserID, account.Result.ID)
	require.Contains(t, account.Result.ShareURL, "https://t.me/zinusya_test_bot?start=partner_")
	code, data = invitationAPIRequest(
		t,
		f,
		a,
		"invitation_context",
		"invite-policy",
		map[string]any{"event_id": f.EventID},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	var policy struct {
		Result db.PairInvitationContext `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &policy))
	require.Equal(t, "Europe/Minsk", policy.Result.Timezone)
	require.Equal(t, f.Entry.Version, policy.Result.ExpectedVersion)
	invite := db.PairInvitationRequest{
		EventID:         f.EventID,
		TargetUserID:    account.Result.ID,
		ExpectedVersion: policy.Result.ExpectedVersion,
	}
	code, data = invitationAPIRequest(t, f, a, "invite", "api-invite", invite)
	require.Equal(t, http.StatusOK, code, string(data))
	var created struct {
		Result db.PairResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &created))
	require.Equal(t, policy.Result.ExpiresAt, created.Result.Invitation.ExpiresAt)
	code, data = invitationAPIRequest(t, f, b, "invitations", "incoming", map[string]any{})
	require.Equal(t, http.StatusOK, code, string(data))
	var inbox struct {
		Result struct {
			Invitations []db.PairInvitationView `json:"invitations"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &inbox))
	require.Len(t, inbox.Result.Invitations, 1)
	view := inbox.Result.Invitations[0]
	require.Equal(t, registrations.Follower, view.RequiredRole)
	require.Equal(t, f.A.UserID, view.InviterUserID)
	require.NotEmpty(t, view.EventName)
	require.NotContains(t, string(data), "price_minor")
	code, data = invitationAPIRequest(t, f, other, "invitations", "other-inbox", map[string]any{"state": "all"})
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"invitations":[]`)
	decision := db.PairInvitationDecision{
		EventID:         view.Invitation.EventID,
		InvitationID:    view.Invitation.ID,
		ExpectedVersion: view.Invitation.Version,
		Role:            view.RequiredRole,
	}
	code, data = invitationAPIRequest(t, f, other, "accept", "other-accept", decision)
	require.Equal(t, http.StatusForbidden, code, string(data))
	code, data = invitationAPIRequest(t, f, b, "accept", "api-accept", decision)
	require.Equal(t, http.StatusOK, code, string(data))
	var accepted struct {
		Result db.PairResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &accepted))
	require.Equal(t, "accepted", accepted.Result.Invitation.State)
	require.NotNil(t, accepted.Result.Pair)
	entry, err := f.H.store.Registration(ctx, f.B.EventScope, f.EventID, f.B.UserID)
	require.NoError(t, err)
	require.Equal(t, registrations.Follower, entry.Role)
	require.Nil(t, entry.Pass)
	status := map[string]any{"protocol_version": 1, "operation_id": "api-invite"}
	code, data = adminRequest(t, f.H, a, http.MethodPost, "/internal/agent/pairs/invite/status", status)
	require.Equal(t, http.StatusOK, code, string(data))
	var original struct {
		Result db.PairResult `json:"result"`
	}
	require.NoError(t, json.Unmarshal(data, &original))
	require.Equal(t, created.Result, original.Result)
	code, data = invitationAPIRequest(
		t,
		f,
		a,
		"invitations",
		"outgoing-history",
		map[string]any{"direction": "outgoing", "state": "all"},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), `"state":"accepted"`)
	f.H.stop(t)
	f.H.start(t)
	status["operation_id"] = "api-accept"
	for _, suffix := range []string{"status", "reconcile"} {
		code, data = adminRequest(t, f.H, b, http.MethodPost, "/internal/agent/pairs/accept/"+suffix, status)
		require.Equal(t, http.StatusOK, code, string(data))
		require.NoError(t, json.Unmarshal(data, &original))
		require.Equal(t, accepted.Result, original.Result)
	}
	code, data = adminRequest(t, f.H, b, http.MethodPost, "/internal/agent/pairs/revoke/status", status)
	require.Equal(t, http.StatusOK, code, string(data))
	require.Contains(t, string(data), "not_found")
	require.NoError(t, f.H.store.RevokeUser(ctx, f.B.UserID, true))
	code, data = adminRequest(t, f.H, b, http.MethodPost, "/internal/agent/pairs/accept/status", status)
	require.Equal(t, http.StatusForbidden, code, string(data))
}

func TestPairInvitationAPICleanupWorksWhenModuleIsDisabled(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"decline", "revoke"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := setupPair(t)
			a, b := pairUserToken(f, f.A), pairUserToken(f, f.B)
			code, data := invitationAPIRequest(
				t,
				f,
				a,
				"invite",
				"create",
				db.PairInvitationRequest{
					EventID:         f.EventID,
					TargetUserID:    f.B.UserID,
					ExpectedVersion: f.Entry.Version,
				},
			)
			require.Equal(t, http.StatusOK, code, string(data))
			var created struct {
				Result db.PairResult `json:"result"`
			}
			require.NoError(t, json.Unmarshal(data, &created))
			_, err := f.H.store.Pool().Exec(ctx, "UPDATE shuttle_events SET modules='{}' WHERE id=$1", f.EventID)
			require.NoError(t, err)
			args := db.PairInvitationDecision{
				EventID:         f.EventID,
				InvitationID:    created.Result.Invitation.ID,
				ExpectedVersion: 1,
				Role:            registrations.Follower,
			}
			code, data = invitationAPIRequest(t, f, b, "accept", "disabled-accept", args)
			require.Equal(t, http.StatusOK, code, string(data))
			require.Contains(t, string(data), `"status":"rejected"`)
			args.Role = ""
			actor := b
			if kind == "revoke" {
				actor = a
			}
			code, data = invitationAPIRequest(t, f, actor, kind, "cleanup", args)
			require.Equal(t, http.StatusOK, code, string(data))
			var closed struct {
				Result db.PairResult `json:"result"`
			}
			require.NoError(t, json.Unmarshal(data, &closed))
			require.Nil(t, closed.Result.Pair)
			entry, err := f.H.store.Registration(ctx, f.A.EventScope, f.EventID, f.A.UserID)
			require.NoError(t, err)
			require.Equal(t, f.Entry.Version+2, entry.Version)
		})
	}
}
