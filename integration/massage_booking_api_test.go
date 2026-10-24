package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

const massageOwnerAPI = "/internal/agent/massage/"

func massageAgentPreparation[A any](
	t *testing.T,
	f paymentFixture,
	token, method string,
	operation uuid.UUID,
	args A,
) (uuid.UUID, map[string]any) {
	t.Helper()
	body := map[string]any{"protocol_version": 1, "operation_id": operation, "arguments": args}
	code, raw := adminRequest(t, f.H, token, http.MethodPost, massageOwnerAPI+method+"/prepare", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	var preparation struct {
		Status        string `json:"status"`
		Description   string `json:"description"`
		Preconditions struct {
			ProposalID uuid.UUID `json:"proposal_id"`
		} `json:"preconditions"`
	}
	require.NoError(t, json.Unmarshal(raw, &preparation))
	require.Equal(t, "prepared", preparation.Status)
	require.NotEmpty(t, preparation.Description)
	require.LessOrEqual(t, len(preparation.Description), 4000)
	require.NotEqual(t, uuid.Nil, preparation.Preconditions.ProposalID)
	body["idempotency_key"] = operation
	body["confirmation"] = map[string]any{
		"mode":          "exact_parameters",
		"preconditions": map[string]any{"proposal_id": preparation.Preconditions.ProposalID},
	}
	return preparation.Preconditions.ProposalID, body
}

func massageAgentResult(t *testing.T, raw []byte) db.MassageBooking {
	t.Helper()
	var result struct {
		Status string            `json:"status"`
		Result db.MassageBooking `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "succeeded", result.Status, string(raw))
	return result.Result
}

func TestMassageBookingAgentHTTPMixedHandoffsAndOriginalStatus(t *testing.T) {
	t.Parallel()
	f, owner, other, schedule := massageBookingFixture(t, 2)
	args := massageBookingArgs(f, schedule, 0)
	operation := uuid.New()
	proposal, body := massageAgentPreparation(t, f, f.Owner, "book", operation, args)
	session := nativeMassageSession(t, f, owner, 11002)
	deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	native, err := f.H.store.BookMiniAppMassage(t.Context(), deployment, session, uuid.New(), proposal, args)
	require.NoError(t, err)
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"book", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, native, massageAgentResult(t, raw))
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"operation", map[string]any{
		"protocol_version": 1, "operation_id": uuid.NewString(), "arguments": map[string]any{"operation_id": operation},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	var receipt struct {
		Status string                     `json:"status"`
		Result db.MassageOperationReceipt `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Equal(t, "succeeded", receipt.Status)
	require.Equal(t, "book", receipt.Result.Kind)
	require.Equal(t, db.MassageSourceAgent, receipt.Result.Source)
	require.Equal(t, db.MassageSourceMiniApp, receipt.Result.Result.Source)
	cancelArgs := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       native.ID,
		ExpectedVersion: native.Version,
	}
	cancelOp := uuid.New()
	cancelProposal, cancelBody := massageAgentPreparation(t, f, f.Owner, "cancel", cancelOp, cancelArgs)
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"cancel", cancelBody)
	require.Equal(t, http.StatusOK, code, string(raw))
	cancelled := massageAgentResult(t, raw)
	require.Equal(t, "cancelled", cancelled.State)
	recovered, err := f.H.store.CancelMiniAppMassage(
		t.Context(),
		deployment,
		session,
		uuid.New(),
		cancelProposal,
		cancelArgs,
	)
	require.NoError(t, err)
	require.Equal(t, cancelled, recovered)
	code, raw = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		massageOwnerAPI+"book/status",
		map[string]any{"protocol_version": 1, "operation_id": operation},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, native, massageAgentResult(t, raw))
	code, raw = adminRequest(
		t,
		f.H,
		f.Owner,
		http.MethodPost,
		massageOwnerAPI+"cancel/status",
		map[string]any{"protocol_version": 1, "operation_id": operation},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var missing struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.Unmarshal(raw, &missing))
	require.Equal(t, "not_found", missing.Status, "An unrelated operation phase must not look successful")
	get := map[string]any{
		"protocol_version": 1,
		"operation_id":     uuid.NewString(),
		"arguments":        map[string]any{"event_id": f.Event.ID, "booking_id": native.ID},
	}
	code, raw = adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"get", get)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Equal(t, cancelled, massageAgentResult(t, raw))
	code, raw = adminRequest(t, f.H, f.Ambassador, http.MethodPost, massageOwnerAPI+"get", get)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	_, err = f.H.store.OwnMassageBooking(t.Context(), other, f.Event.ID, native.ID)
	require.Error(t, err)
}

func TestMassageBookingAgentHTTPRejectsMissingChangedAndUnownedApprovals(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 2)
	args := massageBookingArgs(f, schedule, 0)
	operation := uuid.New()
	proposal, _ := massageAgentPreparation(t, f, f.Owner, "book", operation, args)
	for _, kind := range []string{"mode", "key", "proposal", "changed"} {
		input := args
		confirmation := map[string]any{
			"mode":          "exact_parameters",
			"preconditions": map[string]any{"proposal_id": proposal},
		}
		key := operation
		switch kind {
		case "mode":
			confirmation["mode"] = "none"
		case "key":
			key = uuid.New()
		case "proposal":
			confirmation["preconditions"] = map[string]any{"proposal_id": uuid.New()}
		case "changed":
			input.Selection.SpecialistID = "beth"
		}
		code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"book", map[string]any{
			"protocol_version": 1,
			"operation_id":     operation,
			"idempotency_key":  key,
			"arguments":        input,
			"confirmation":     confirmation,
		})
		if kind == "mode" || kind == "key" {
			require.GreaterOrEqual(t, code, http.StatusBadRequest, string(raw))
			require.Less(t, code, http.StatusInternalServerError, string(raw))
		} else {
			require.Equal(t, http.StatusOK, code, string(raw))
			var rejected struct {
				Status string `json:"status"`
			}
			require.NoError(t, json.Unmarshal(raw, &rejected))
			require.Equal(t, "rejected", rejected.Status)
		}
	}
	code, raw := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		massageOwnerAPI+"book/prepare",
		map[string]any{"protocol_version": 1, "operation_id": uuid.NewString(), "arguments": args},
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	page, err := f.H.store.OwnMassageBookings(t.Context(), owner, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, page.Bookings)
}

func TestMassageBookingAgentHTTPBoundsHistoryWithoutSkippingRecords(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 2)
	ctx := t.Context()
	admin := ownerProfileScope(t, f.H, f.AdminID)
	settings, err := f.H.store.Settings(ctx)
	require.NoError(t, err)
	settings.Document.MassageMaxBookingsPerWindow = 20
	_, err = f.H.store.CompareAndSwapSettings(ctx, settings.Version, settings.Document, admin.Subject)
	require.NoError(t, err)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "UPDATE shuttle_events SET name=$2,version=version+1 WHERE id=$1 RETURNIN" +
"G version", f.Event.ID, strings.Repeat("😀", 256)).
			Scan(&f.Event.Version),
	)
	schedule.ExpectedEventVersion = f.Event.Version
	for i := range schedule.Document.Windows {
		schedule.Document.Windows[i].EndsAt = schedule.Document.Windows[i].StartsAt.Add(8 * time.Hour)
	}
	for i := range schedule.Document.Specialists {
		for j := range schedule.Document.Specialists[i].WorkSpans {
			span := &schedule.Document.Specialists[i].WorkSpans[j]
			span.EndsAt = span.StartsAt.Add(8 * time.Hour)
		}
	}

	label := massage.ScheduleLabel{EN: strings.Repeat("😀", 128), RU: strings.Repeat("😀", 128)}
	for i := range schedule.Document.Windows {
		schedule.Document.Windows[i].Name = label
	}
	for i := range schedule.Document.Specialists {
		schedule.Document.Specialists[i].Name = label
	}
	for i := range schedule.Document.Services {
		schedule.Document.Services[i].Name = label
	}
	operation := uuid.New()
	prepared, err := f.H.store.PrepareMassageSchedule(ctx, admin, operation, schedule)
	require.NoError(t, err)
	changed, err := f.H.store.ChangeMassageSchedule(
		ctx,
		admin,
		operation,
		prepared.ID,
		schedule,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	schedule.ExpectedVersion = changed.Version
	const bookings = 20
	for slot := range bookings {
		_, _ = bookMassage(t, f, owner, massageBookingArgs(f, schedule, slot))
	}
	seen := map[uuid.UUID]bool{}
	after := uuid.Nil
	for pageNumber := range bookings {
		code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"list", map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.NewString(),
			"arguments":        map[string]any{"event_id": f.Event.ID, "after": after, "limit": 20},
		})
		require.Equal(t, http.StatusOK, code, string(raw))
		t.Logf("History page %d: %d encoded response bytes", pageNumber, len(raw))
		require.LessOrEqual(t, len(raw), 65536, "Response must fit its advertised Loom limit")
		var page struct {
			Status string                `json:"status"`
			Result db.MassageBookingPage `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &page))
		require.Equal(t, "succeeded", page.Status)
		require.NotEmpty(t, page.Result.Bookings)
		if pageNumber == 0 {
			require.Less(t, len(page.Result.Bookings), bookings, "Byte limit must split this valid large history")
		}
		for _, booking := range page.Result.Bookings {
			require.False(t, seen[booking.ID], "Cursor must not repeat a booking")
			seen[booking.ID] = true
		}
		if page.Result.NextAfter == nil {
			break
		}
		require.NotEqual(t, after, *page.Result.NextAfter)
		after = *page.Result.NextAfter
	}
	require.Len(t, seen, bookings, "Byte pagination must retain every booking")
}
