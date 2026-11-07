package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
)

func massageAgentPending(t *testing.T, f paymentFixture, after uuid.UUID, limit int) db.MassagePendingPage {
	t.Helper()
	code, raw := adminRequest(t, f.H, f.Owner, http.MethodPost, massageOwnerAPI+"pending", map[string]any{
		"protocol_version": 1, "operation_id": uuid.New(),
		"arguments": map[string]any{"event_id": f.Event.ID, "after": after, "limit": limit},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	require.LessOrEqual(t, len(raw), 65536)
	var response struct {
		Result db.MassagePendingPage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	return response.Result
}

func TestMassagePendingDiscoveryBothDirectionsPreservesOriginAndExcludesConsumed(t *testing.T) {
	t.Parallel()
	f, owner, change, session := massageAppFixture(t)
	args := massageBookingArgs(f, change, 0)
	nativeOp := uuid.New()
	before := time.Now().UTC().Add(-time.Second)
	native := massageAppCall[db.MassageBookingPreparation](t, f, session, http.MethodPost,
		massageAppPath(f.Event.ID)+"/book/prepare", map[string]any{"operation_id": nativeOp, "arguments": args})
	agent, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
	require.NoError(t, err)
	page := massageAgentPending(t, f, uuid.Nil, 20)
	require.Len(t, page.Proposals, 2, "Equal terms do not merge separate intended actions")
	var original db.MassagePendingProposal
	for _, item := range page.Proposals {
		require.Equal(t, "book", item.Kind)
		require.NotNil(t, item.Booking)
		require.Equal(t, args, *item.Booking)
		require.Nil(t, item.Cancellation)
		require.True(t, item.CreatedAt.After(before))
		if item.ID == native.ID {
			original = item
			require.Equal(t, db.MassageSourceMiniApp, item.Source)
		} else {
			require.Equal(t, agent.ID, item.ID)
			require.Equal(t, db.MassageSourceAgent, item.Source)
		}
	}
	require.Equal(t, native.ID, original.ID)
	require.True(t, original.ExpiresAt.Equal(native.ExpiresAt))
	_, err = f.H.store.AdoptOwnMassageBooking(t.Context(), owner, uuid.New(), native.ID, args)
	require.NoError(t, err)
	nativePage := massageAppCall[db.MassagePendingPage](t, f, session, http.MethodGet,
		massageAppPath(f.Event.ID)+"/pending", nil)
	require.Equal(t, page, nativePage, "Adoption must not change creation provenance or expiry")
	booking := massageAppCall[db.MassageBooking](t, f, session, http.MethodPost,
		massageAppPath(f.Event.ID)+"/book", map[string]any{
			"operation_id": nativeOp, "arguments": args, "proposal_id": native.ID, "confirmed": true,
		})
	page = massageAgentPending(t, f, uuid.Nil, 20)
	require.Len(t, page.Proposals, 1)
	require.Equal(t, agent.ID, page.Proposals[0].ID)
	cancelArgs := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	cancellation := massageAppCall[db.MassageCancellationPreparation](
		t,
		f,
		session,
		http.MethodPost,
		massageAppPath(
			f.Event.ID,
		)+"/cancel/prepare",
		map[string]any{"operation_id": uuid.New(), "arguments": cancelArgs},
	)
	page = massageAgentPending(t, f, uuid.Nil, 20)
	require.Len(t, page.Proposals, 2)
	for _, item := range page.Proposals {
		if item.ID == cancellation.ID {
			require.Equal(t, "cancel", item.Kind)
			require.Nil(t, item.Booking)
			require.NotNil(t, item.Cancellation)
			require.Equal(t, cancelArgs, *item.Cancellation)
			require.Equal(t, db.MassageSourceMiniApp, item.Source)
		}
	}
	var tasks int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks))
	require.Zero(t, tasks, "Reading and preparing never starts Loom")
}

func TestMassagePendingRemainsDiscoverableAfterEntitlementLossUntilExpiry(t *testing.T) {
	t.Parallel()
	f, owner, change, session := massageAppFixture(t)
	args := massageBookingArgs(f, change, 0)
	proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage'),"+
			"version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(
			t.Context(),
			"UPDATE shuttle_registrations SET state='waitlisted' WHERE event_id=$1 AND user_id=$2",
			f.Event.ID,
			owner.UserID,
		)
	require.NoError(t, err)
	choices := massageAppCall[events.ContextList](t, f, session, http.MethodGet, "/app/massage/events", nil)
	require.Len(t, choices.Events, 1, "Pending-only event remains discoverable")
	page := massageAppCall[db.MassagePendingPage](
		t,
		f,
		session,
		http.MethodGet,
		massageAppPath(f.Event.ID)+"/pending",
		nil,
	)
	require.Len(t, page.Proposals, 1)
	require.Equal(t, proposal.ID, page.Proposals[0].ID)
	require.Equal(t, db.MassageSourceAgent, page.Proposals[0].Source)
	require.Equal(t, page, massageAgentPending(t, f, uuid.Nil, 20))
	_, err = f.H.store.AdoptOwnMassageBooking(t.Context(), owner, uuid.New(), proposal.ID, args)
	require.Error(t, err, "Informational discovery does not restore booking entitlement")
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_preparations SET expires_at=clock_timestamp()-interval '1 "+
			"second' WHERE id=$1", proposal.ID)
	require.NoError(t, err)
	require.Empty(t, massageAgentPending(t, f, uuid.Nil, 20).Proposals)
	choices = massageAppCall[events.ContextList](t, f, session, http.MethodGet, "/app/massage/events", nil)
	require.Empty(t, choices.Events, "Expired pending-only event need not remain in picker")
}

func TestMassagePendingPagesAreBoundedOwnerScopedAndSessionAware(t *testing.T) {
	t.Parallel()
	f, owner, change, session := massageAppFixture(t)
	args := massageBookingArgs(f, change, 0)
	for range 21 {
		_, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
		require.NoError(t, err)
	}
	seen := map[uuid.UUID]bool{}
	after := uuid.Nil
	for range 21 {
		page := massageAgentPending(t, f, after, 1)
		require.NotEmpty(t, page.Proposals)
		require.LessOrEqual(t, len(page.Proposals), 20)
		for _, item := range page.Proposals {
			require.False(t, seen[item.ID])
			seen[item.ID] = true
		}
		if page.NextAfter == nil {
			break
		}
		after = *page.NextAfter
	}
	require.Len(t, seen, 21)
	code, raw := adminRequest(t, f.H, f.Ambassador, http.MethodPost, massageOwnerAPI+"pending", map[string]any{
		"protocol_version": 1, "operation_id": uuid.New(), "arguments": map[string]any{"event_id": f.Event.ID},
	})
	require.Equal(t, http.StatusOK, code, string(raw))
	var foreign struct {
		Result db.MassagePendingPage `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &foreign))
	require.Empty(t, foreign.Result.Proposals)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		massageAppPath(f.Event.ID)+"/pending?limit=21",
		"",
		session.Token,
		"",
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodPost, "/app/session/logout", "{}", session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodGet, massageAppPath(f.Event.ID)+"/pending", "", session.Token, "")
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}
