package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestAgentIdentityExportDeliversOnlyToRequestingOrganizer(t *testing.T) {
	t.Parallel()
	const path = "/internal/agent/registrations/send_identity_export"
	const organizerChat = 98901
	const passport = "SYNTHETIC-IDENTITY-792"
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.registrationDetails = detailsProtector(t, 88)
	h := setup(t, p)
	memberID, member := registrationUser(t, h, 98902, false)
	adminID, admin := registrationUser(t, h, organizerChat, true)
	scope := paymentScope(t, h, adminID, true)
	event := identityExportEvent(t, h, scope, admin, registrationdetails.Requirements{PassportNumber: true})
	saveIdentityExportDetails(t, h, detailsAppSession(t, h, 98902, 151), 3, "Synthetic participant", passport, "NL")
	assignIdentityExportParticipant(t, h, admin, member, event, registrations.Leader)
	grant, err := h.store.ChangeAuthority(t.Context(), scope.EventScope, scope.Subject, uuid.NewString(),
		db.AuthorityChange{EventID: event.ID, UserID: adminID, Role: db.AuthorityAdministrator}, uuid.Nil)
	require.NoError(t, err)
	p.mu.Lock()
	p.adminRole = false
	admin = p.token(scope.Subject)
	p.mu.Unlock()
	h.iam.Invalidate(scope.Subject)
	h.iam.Invalidate(paymentScope(t, h, memberID, false).Subject)

	request := func(op string) map[string]any {
		return map[string]any{"protocol_version": 1, "operation_id": op, "arguments": map[string]any{"event_id": event.ID}}
	}
	code, _ := adminRequest(t, h, member, http.MethodPost, path+"/prepare", request(uuid.NewString()))
	require.Equal(t, http.StatusForbidden, code)
	redirect := request(uuid.NewString())
	redirect["arguments"] = map[string]any{"event_id": event.ID, "chat_id": 98902}
	code, _ = adminRequest(t, h, admin, http.MethodPost, path+"/prepare", redirect)
	require.Equal(t, http.StatusUnprocessableEntity, code)

	for i, want := range []string{"sent", "delivery_unknown"} {
		op := uuid.NewString()
		body := request(op)
		var raw []byte
		code, raw = adminRequest(t, h, admin, http.MethodPost, path+"/prepare", body)
		require.Equal(t, http.StatusOK, code, string(raw))
		var proposal assignmentProposal
		require.NoError(t, json.Unmarshal(raw, &proposal))
		require.Equal(t, "prepared", proposal.Status)
		require.NotEqual(t, uuid.Nil, proposal.Preconditions.ProposalID)
		require.NotContains(t, string(raw), passport)
		body["idempotency_key"] = op
		code, _ = adminRequest(t, h, admin, http.MethodPost, path, body)
		require.GreaterOrEqual(t, code, 400)
		p.mu.Lock()
		prior := len(p.documentSent)
		p.loseDocumentSend = i == 1
		p.mu.Unlock()
		require.Equal(t, i, prior)
		body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": proposal.Preconditions}
		for _, suffix := range []string{"", "", "/status"} {
			call := body
			if suffix != "" {
				call = map[string]any{"protocol_version": 1, "operation_id": op}
			}
			code, raw = adminRequest(t, h, admin, http.MethodPost, path+suffix, call)
			require.Equal(t, http.StatusOK, code, string(raw))
			var result struct {
				Status string         `json:"status"`
				Result map[string]any `json:"result"`
			}
			require.NoError(t, json.Unmarshal(raw, &result))
			require.Equal(t, "succeeded", result.Status)
			require.Equal(t, map[string]any{"delivery_status": want}, result.Result)
			require.NotContains(t, string(raw), passport)
		}
		p.mu.Lock()
		sent := p.documentSent
		p.mu.Unlock()
		require.Len(t, sent, i+1)
		require.EqualValues(t, organizerChat, sent[i].ChatID)
		files := readPassAdminWorkbook(t, sent[i].Content)
		sheets := readPassAdminWorkbook(t, []byte(files["participants.xlsx"]))
		require.Contains(t, sheets["xl/worksheets/sheet1.xml"], passport)
	}

	op := uuid.NewString()
	body := request(op)
	code, raw := adminRequest(t, h, admin, http.MethodPost, path+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var prepared assignmentProposal
	require.NoError(t, json.Unmarshal(raw, &prepared))
	_, err = h.store.ChangeAuthority(
		t.Context(),
		scope.EventScope,
		scope.Subject,
		uuid.NewString(),
		db.AuthorityChange{
			EventID:         event.ID,
			UserID:          adminID,
			Role:            db.AuthorityNone,
			ExpectedVersion: grant.Version,
		},
		uuid.Nil,
	)
	require.NoError(t, err)
	body["idempotency_key"] = op
	body["confirmation"] = map[string]any{"mode": "exact_parameters", "preconditions": prepared.Preconditions}
	code, _ = adminRequest(t, h, admin, http.MethodPost, path, body)
	require.Equal(t, http.StatusForbidden, code)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.documentSent, 2)
}
