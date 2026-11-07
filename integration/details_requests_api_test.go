package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func detailsOutput(t *testing.T, raw []byte, schema *huma.Schema, target any) {
	t.Helper()
	var receipt struct {
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "succeeded", receipt.Status, string(raw))
	var output any
	require.NoError(t, json.Unmarshal(receipt.Result, &output))
	require.NotNil(t, schema)
	validation := &huma.ValidateResult{}
	huma.Validate(
		huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer),
		schema,
		huma.NewPathBuffer(nil, 0),
		huma.ModeReadFromServer,
		output,
		validation,
	)
	require.Empty(t, validation.Errors)
	require.NoError(t, json.Unmarshal(receipt.Result, target))
	require.NotContains(t, string(raw), "SYNTHETIC-PASSPORT")
	require.NotContains(t, string(raw), "Synthetic Legal Name")
}

func TestDetailsRequestAPIRecoversHistoricalReceiptAndReadsCurrentState(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 76)
	h := setup(t, p)
	_, admin := registrationUser(t, h, 95501, true)
	owner, token := registrationUser(t, h, 95502, false)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	schemas := detailsSchemas(t, h, token)
	code, raw := detailsRequest(t, h, token, "readiness", uuid.New(), map[string]any{"event_id": event.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var readiness db.DetailsReadiness
	detailsOutput(t, raw, schemas["readiness"], &readiness)
	require.False(t, readiness.Ready)
	require.Len(t, readiness.MissingFields, 2)
	operation := uuid.New()
	args := db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version}
	code, raw = detailsRequest(t, h, token, "begin_request", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	var opened db.DetailsRequestStatus
	detailsOutput(t, raw, schemas["begin_request"], &opened)
	require.Equal(t, "pending", opened.State)
	completeRequiredDetails(t, h, owner)
	h.stop(t)
	h.start(t)
	code, raw = detailsRequest(t, h, token, "begin_request", operation, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	var repeated db.DetailsRequestStatus
	detailsOutput(t, raw, schemas["begin_request"], &repeated)
	require.Equal(t, opened, repeated)
	code, raw = adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		detailsAPI+"request_operation",
		map[string]any{"protocol_version": 1, "operation_id": operation},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	detailsOutput(t, raw, schemas["request_operation"], &repeated)
	require.Equal(t, opened, repeated)
	code, raw = detailsRequest(t, h, token, "request", uuid.New(), map[string]any{"request_id": opened.Request.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	var current db.DetailsRequestStatus
	detailsOutput(t, raw, schemas["request"], &current)
	require.Equal(t, "completed", current.State)
	require.Empty(t, current.MissingFields)
	code, raw = detailsRequest(t, h, token, "readiness", uuid.New(), map[string]any{"event_id": event.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	detailsOutput(t, raw, schemas["readiness"], &readiness)
	require.True(t, readiness.Ready)
	var count int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_registrations WHERE user_id=$1", owner).
			Scan(&count),
	)
	require.Zero(t, count)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.createKeys)
}

func TestDetailsRequestAPIPaginatesAndRejectsForeignStaleRevokedAndInvalidRequests(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.registrationDetails = detailsProtector(t, 77)
	h := setup(t, p)
	_, admin := registrationUser(t, h, 95511, true)
	owner, token := registrationUser(t, h, 95512, false)
	_, other := registrationUser(t, h, 95513, false)
	scope := ownerProfileScope(t, h, owner)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	first := beginDetailsRequest(t, h, scope, event, uuid.New())
	event2 := requireEventDetails(t, h, registrationEvent(t, h, admin))
	second := beginDetailsRequest(t, h, scope, event2, uuid.New())
	schemas := detailsSchemas(t, h, token)
	var page struct {
		Requests []db.DetailsRequestStatus `json:"requests"`
		Next     *uuid.UUID                `json:"next"`
	}
	ids := map[uuid.UUID]bool{}
	after := uuid.Nil
	for range 3 {
		code, raw := detailsRequest(t, h, token, "requests", uuid.New(), map[string]any{"limit": 1, "after": after})
		require.Equal(t, http.StatusOK, code, string(raw))
		page.Next = nil
		detailsOutput(t, raw, schemas["requests"], &page)
		for _, entry := range page.Requests {
			require.False(t, ids[entry.Request.ID])
			ids[entry.Request.ID] = true
		}
		if page.Next == nil {
			break
		}
		after = *page.Next
	}
	require.Equal(t, map[uuid.UUID]bool{first.Request.ID: true, second.Request.ID: true}, ids)
	require.Nil(t, page.Next)
	code, raw := detailsRequest(t, h, other, "request", uuid.New(), map[string]any{"request_id": first.Request.ID})
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw = detailsRequest(t, h, other, "request_operation", uuid.New(), map[string]any{"operation_id": uuid.New()})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "not_found")
	code, raw = detailsRequest(
		t,
		h,
		token,
		"begin_request",
		uuid.New(),
		db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version - 1},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), "terminal_rejection")
	code, _ = detailsRequest(t, h, token, "requests", uuid.New(), map[string]any{"limit": 21})
	require.Equal(t, http.StatusUnprocessableEntity, code)
	code, _ = adminRequest(
		t,
		h,
		token,
		http.MethodPost,
		detailsAPI+"begin_request",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.New(),
			"idempotency_key":  uuid.New(),
			"arguments":        db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version},
		},
	)
	require.Equal(t, http.StatusBadRequest, code)
	_, err := h.store.SetUserRevoked(t.Context(), owner, "details-requests-api-fixture")
	require.NoError(t, err)
	for _, method := range []string{"readiness", "begin_request", "request", "request_operation", "requests"} {
		args := map[string]any{}
		switch method {
		case "readiness":
			args["event_id"] = event.ID
		case "begin_request":
			args["event_id"], args["expected_event_version"] = event.ID, event.Version
		case "request":
			args["request_id"] = first.Request.ID
		case "request_operation":
			args["operation_id"] = uuid.New()
		}
		code, _ = detailsRequest(t, h, token, method, uuid.New(), args)
		require.Equal(t, http.StatusForbidden, code, method)
	}
}

func TestDetailsRequestPromptIsRedactedAndDoesNotConsumeUnrelatedMessage(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	_, admin := registrationUser(t, h, 95521, true)
	owner, _ := registrationUser(t, h, 456, false)
	scope := ownerProfileScope(t, h, owner)
	event := requireEventDetails(t, h, registrationEvent(t, h, admin))
	opened := beginDetailsRequest(t, h, scope, event, uuid.New())
	id := h.submitText(t, 95521, "Tell me about the event first")
	h.await(t, id, "completed")
	p.mu.Lock()
	text := p.createdInputs[len(p.createdInputs)-1]
	p.mu.Unlock()
	require.Contains(t, text, opened.Request.ID.String())
	require.Contains(t, text, "registration data expectations")
	require.Contains(t, text, "pending")
	require.Equal(t, "Tell me about the event first", promptUserText(text))
	unchanged, err := h.store.DetailsRequest(t.Context(), scope, opened.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", unchanged.State)
	status, err := h.store.MutateRegistrationDetails(
		t.Context(),
		scope,
		registrationdetails.Mutation{
			OperationID: uuid.New(),
			Field:       registrationdetails.LegalName,
			Value:       "Synthetic Legal Name",
		},
		detailsProtector(t, 78),
	)
	require.NoError(t, err)
	_, err = h.store.MutateRegistrationDetails(
		t.Context(),
		scope,
		registrationdetails.Mutation{
			OperationID:     uuid.New(),
			ExpectedVersion: status.Version,
			Field:           registrationdetails.PassportNumber,
			Value:           "SYNTHETIC-PASSPORT-95521",
		},
		detailsProtector(t, 78),
	)
	require.NoError(t, err)
	id = h.submitText(t, 95522, "Continue")
	h.await(t, id, "completed")
	p.mu.Lock()
	text = p.createdInputs[len(p.createdInputs)-1]
	p.mu.Unlock()
	require.Contains(t, text, opened.Request.ID.String())
	require.Contains(t, text, "completed")
	require.NotContains(t, text, "SYNTHETIC-PASSPORT")
	require.NotContains(t, text, "Synthetic Legal Name")
	var count int
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_registrations WHERE user_id=$1", owner).
			Scan(&count),
	)
	require.Zero(t, count)
}
