package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

func detailsRequestFixture(t *testing.T) (*harness, db.ProfileScope, events.Event) {
	t.Helper()
	h := setup(t, newPeers(t))
	_, admin := registrationUser(t, h, 95401, true)
	owner, _ := registrationUser(t, h, 95402, false)
	event := registrationEvent(t, h, admin)
	event.Document.Registration.ClosesAt = time.Now().UTC().Truncate(time.Microsecond).Add(4 * time.Hour)
	event, err := h.store.ChangeEvent(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		"details-request-fixture",
		uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document},
	)
	require.NoError(t, err)
	return h, ownerProfileScope(t, h, owner), requireEventDetails(t, h, event)
}

func beginDetailsRequest(
	t *testing.T,
	h *harness,
	scope db.ProfileScope,
	event events.Event,
	operation uuid.UUID,
) db.DetailsRequestStatus {
	t.Helper()
	result, err := h.store.BeginDetailsRequest(
		t.Context(),
		scope,
		operation,
		db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version},
	)
	require.NoError(t, err)
	return result
}

func TestDetailsRequestsKeepOneDeadlineAndFulfillWithoutStartingAgent(t *testing.T) {
	t.Parallel()
	h, scope, event := detailsRequestFixture(t)
	operation := uuid.New()
	first := beginDetailsRequest(t, h, scope, event, operation)
	require.Equal(t, "pending", first.State)
	require.Equal(t, 2*time.Hour, first.Request.ExpiresAt.Sub(first.Request.CreatedAt))
	require.Equal(t, "Europe/Minsk", first.Request.EventTimezone)
	require.Equal(
		t,
		[]registrationdetails.Field{registrationdetails.LegalName, registrationdetails.PassportNumber},
		first.MissingFields,
	)
	repeated := beginDetailsRequest(t, h, scope, event, uuid.New())
	require.Equal(t, first.Request, repeated.Request)
	require.Equal(t, first, beginDetailsRequest(t, h, scope, event, operation))
	completeRequiredDetails(t, h, scope.UserID)
	ready, err := h.store.DetailsRequest(t.Context(), scope, first.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", ready.State)
	require.Empty(t, ready.MissingFields)
	require.NotNil(t, ready.Request.CompletedDetailsVersion)
	require.EqualValues(t, 2, *ready.Request.CompletedDetailsVersion)
	require.Equal(t, first.Request.ExpiresAt, ready.Request.ExpiresAt)
	h.stop(t)
	h.start(t)
	reopened, err := h.store.DetailsRequest(t.Context(), scope, first.Request.ID)
	require.NoError(t, err)
	require.Equal(t, ready.Request, reopened.Request)
	historical, err := h.store.DetailsRequestOperation(t.Context(), scope, operation)
	require.NoError(t, err)
	require.Equal(t, first, historical, "Operation recovery is historical, not a current status read")
	var rows, receipts []byte
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(r)),'[]') FROM shuttle_registration_details_requests r`).
			Scan(&rows),
	)
	require.NoError(
		t,
		h.store.Pool().
			QueryRow(t.Context(), "SELECT coalesce(jsonb_agg(result),'[]')"+
				" FROM shuttle_registration_details_request_operations").
			Scan(&receipts),
	)
	require.NotContains(t, string(rows), "SYNTHETIC-REGISTRATION-DETAIL")
	require.NotContains(t, string(receipts), "SYNTHETIC-REGISTRATION-DETAIL")
	var registrations int
	require.NoError(
		t,
		h.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_registrations`).Scan(&registrations),
	)
	require.Zero(t, registrations)
	h.peers.mu.Lock()
	defer h.peers.mu.Unlock()
	require.Empty(t, h.peers.createKeys)
}

func TestDetailsRequestsPreserveHistoricalReceiptsAcrossExpiryAndEventChanges(t *testing.T) {
	t.Parallel()
	h, scope, event := detailsRequestFixture(t)
	operation := uuid.New()
	first := beginDetailsRequest(t, h, scope, event, operation)
	_, err := h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_registration_details_requests"+
			" SET created_at=clock_timestamp()-interval '3 hours',"+
			"expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", first.Request.ID)
	require.NoError(t, err)
	expired, err := h.store.DetailsRequest(t.Context(), scope, first.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "expired", expired.State)
	require.Equal(t, first, beginDetailsRequest(t, h, scope, event, operation))
	replacement := beginDetailsRequest(t, h, scope, event, uuid.New())
	require.NotEqual(t, first.Request.ID, replacement.Request.ID)
	event.Document.Name += " revised"
	updated, err := h.store.ChangeEvent(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		"details-request-fixture",
		uuid.NewString(),
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: event.Document},
	)
	require.NoError(t, err)
	_, err = h.store.BeginDetailsRequest(
		t.Context(),
		scope,
		uuid.New(),
		db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version},
	)
	require.ErrorIs(t, err, db.ErrConflict)
	newest := beginDetailsRequest(t, h, scope, updated, uuid.New())
	require.NotEqual(t, replacement.Request.ID, newest.Request.ID)
	superseded, err := h.store.DetailsRequest(t.Context(), scope, replacement.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "superseded", superseded.State)
	completeRequiredDetails(t, h, scope.UserID)
	expired, err = h.store.DetailsRequest(t.Context(), scope, first.Request.ID)
	require.NoError(t, err)
	require.NotEqual(t, "completed", expired.State)
	current, err := h.store.DetailsRequest(t.Context(), scope, newest.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", current.State)
	_, err = h.store.BeginDetailsRequest(
		t.Context(),
		scope,
		operation,
		db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: updated.Version},
	)
	require.ErrorIs(t, err, db.ErrConflict)
	require.Equal(t, first, beginDetailsRequest(t, h, scope, event, operation))
}

func TestDetailsRequestsAreOwnerBoundAndConcurrentBeginsReuseOneExpectation(t *testing.T) {
	t.Parallel()
	h, scope, event := detailsRequestFixture(t)
	ctx := t.Context()
	type outcome struct {
		result db.DetailsRequestStatus
		err    error
	}
	outcomes := make(chan outcome, 2)
	for range 2 {
		go func() {
			result, err := h.store.BeginDetailsRequest(
				ctx,
				scope,
				uuid.New(),
				db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version},
			)
			outcomes <- outcome{result, err}
		}()
	}
	a, b := <-outcomes, <-outcomes
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	require.Equal(t, a.result.Request, b.result.Request)
	other, _ := registrationUser(t, h, 95403, false)
	otherScope := ownerProfileScope(t, h, other)
	_, err := h.store.DetailsRequest(ctx, otherScope, a.result.Request.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, h.store.RevokeUser(ctx, scope.UserID, true))
	_, err = h.store.DetailsRequest(ctx, scope, a.result.Request.ID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = h.store.BeginDetailsRequest(
		ctx,
		scope,
		uuid.New(),
		db.DetailsRequestStart{EventID: event.ID, ExpectedEventVersion: event.Version},
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestDetailsRequestFulfillmentRollsBackWithPrivateWriteAndReceipt(t *testing.T) {
	t.Parallel()
	h, scope, event := detailsRequestFixture(t)
	request := beginDetailsRequest(t, h, scope, event, uuid.New())
	protector := detailsProtector(t, 76)
	named, err := h.store.MutateRegistrationDetails(
		t.Context(),
		scope,
		registrationdetails.Mutation{
			OperationID: uuid.New(),
			Field:       registrationdetails.LegalName,
			Value:       "Synthetic Legal Name",
		},
		protector,
	)
	require.NoError(t, err)
	_, err = h.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION refuse_details_fulfillment() RETURNS trigger LANGUAGE plpgsql AS $$ "+
			"BEGIN RAISE EXCEPTION 'fixture rejects fulfillment';END $$;\nCREATE TRIGGER "+
			"refuse_details_fulfillment BEFORE UPDATE ON shuttle_registration_details_requests "+
			"FOR EACH ROW EXECUTE FUNCTION refuse_details_fulfillment()")
	require.NoError(t, err)
	mutation := registrationdetails.Mutation{
		OperationID:     uuid.New(),
		ExpectedVersion: named.Version,
		Field:           registrationdetails.PassportNumber,
		Value:           "SYNTHETIC-PRIVATE-REQUEST",
	}
	_, err = h.store.MutateRegistrationDetails(t.Context(), scope, mutation, protector)
	require.Error(t, err)
	status, err := h.store.RegistrationDetailsStatus(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, named, status)
	_, err = h.store.RegistrationDetailsOperation(t.Context(), scope, mutation.OperationID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	pending, err := h.store.DetailsRequest(t.Context(), scope, request.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", pending.State)
	_, err = h.store.Pool().
		Exec(t.Context(), `DROP TRIGGER refuse_details_fulfillment ON shuttle_registration_details_requests`)
	require.NoError(t, err)
	_, err = h.store.MutateRegistrationDetails(t.Context(), scope, mutation, protector)
	require.NoError(t, err)
	fulfilled, err := h.store.DetailsRequest(t.Context(), scope, request.Request.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", fulfilled.State)
	raw, err := json.Marshal(fulfilled)
	require.NoError(t, err)
	require.NotContains(t, string(raw), mutation.Value)
}

func TestDetailsRequestsForMultipleEventsFulfillWithSeparateDeadlines(t *testing.T) {
	t.Parallel()
	h, scope, first := detailsRequestFixture(t)
	_, admin := registrationUser(t, h, 95404, true)
	second := requireEventDetails(t, h, registrationEvent(t, h, admin))
	a := beginDetailsRequest(t, h, scope, first, uuid.New())
	b := beginDetailsRequest(t, h, scope, second, uuid.New())
	require.NotEqual(t, a.Request.ID, b.Request.ID)
	require.Equal(t, second.Document.Registration.ClosesAt, b.Request.ExpiresAt)
	require.Less(t, b.Request.ExpiresAt.Sub(b.Request.CreatedAt), 2*time.Hour)
	completeRequiredDetails(t, h, scope.UserID)
	for _, original := range []db.DetailsRequestStatus{a, b} {
		current, err := h.store.DetailsRequest(t.Context(), scope, original.Request.ID)
		require.NoError(t, err)
		require.Equal(t, "completed", current.State)
		require.Equal(t, original.Request.ExpiresAt, current.Request.ExpiresAt)
	}
}
