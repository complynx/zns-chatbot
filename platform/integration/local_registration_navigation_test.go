package integration_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestLocalRegistrationNavigationHTTPParity(t *testing.T) {
	t.Parallel()
	db, service, local, remote := registrationOperationsFixture(t)
	_, err := service.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "navigation-register", passbooking.Booking{}),
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET finishes_at=now()-interval '1 day',display_order=73,titles=jsonb_build_object('en',repeat('title',1200)) WHERE id='dance';
 INSERT INTO core.pass_events(id,finishes_at,display_order) SELECT 'nav-'||n,now()+interval '2 days',n FROM generate_series(1,21) n;`,
	)
	require.NoError(t, err)
	cursor := ""
	count := 0
	for {
		page, readErr := local.PassEventsPage(t.Context(), "alice", cursor)
		require.NoError(t, readErr)
		other, readErr := remote.PassEventsPage(t.Context(), "alice", cursor)
		require.NoError(t, readErr)
		require.Equal(t, other, page)
		for _, event := range page.Items {
			count++
			require.Zero(t, event.Position)
			if event.ID == "dance" {
				require.Equal(t, passbooking.NavigationOwned, event.Access)
			} else {
				require.Equal(t, passbooking.NavigationPublic, event.Access)
			}
		}
		if !page.More {
			break
		}
		require.Len(t, page.Items, core.ReadPageItems)
		_, readErr = local.PassEventsPage(t.Context(), "bob", page.NextCursor)
		requireCode(t, readErr, "read_cursor_invalid")
		cursor = page.NextCursor
	}
	require.Equal(t, 22, count)
	chunk, err := local.PassEventDetail(t.Context(), "alice", "dance", "")
	require.NoError(t, err)
	require.True(t, chunk.More)
	remoteChunk, err := remote.PassEventDetail(t.Context(), "alice", "dance", "")
	require.NoError(t, err)
	require.Equal(t, remoteChunk, chunk)
	tail, err := local.PassEventDetail(t.Context(), "alice", "dance", chunk.NextCursor)
	require.NoError(t, err)
	remoteTail, err := remote.PassEventDetail(t.Context(), "alice", "dance", chunk.NextCursor)
	require.NoError(t, err)
	require.Equal(t, remoteTail, tail)
	for _, client := range []appclient.Client{local, remote} {
		owned, readErr := client.OwnsPassEvents(t.Context(), "alice", []string{"dance"})
		require.NoError(t, readErr)
		require.True(t, owned)
		owned, readErr = client.OwnsPassEvents(t.Context(), "bob", []string{"dance"})
		require.NoError(t, readErr)
		require.False(t, owned)
		_, readErr = client.OwnsPassEvents(t.Context(), "alice", nil)
		requireCode(t, readErr, "pass_booking_invalid")
		_, readErr = client.PassEventDetail(t.Context(), "bob", "dance", chunk.NextCursor)
		requireCode(t, readErr, "read_cursor_invalid")
		_, readErr = client.PassEventDetail(t.Context(), "alice", "nav-1", chunk.NextCursor)
		requireCode(t, readErr, "read_cursor_invalid")
		_, readErr = client.PassEventsPage(t.Context(), "alice", strings.Repeat("x", 2049))
		requireCode(t, readErr, "read_cursor_invalid")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, readErr = client.PassEventsPage(ctx, "alice", "")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.PassEventDetail(ctx, "alice", "dance", "")
		require.ErrorIs(t, readErr, context.Canceled)
		_, readErr = client.OwnsPassEvents(ctx, "alice", []string{"dance"})
		require.ErrorIs(t, readErr, context.Canceled)
	}
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET titles=jsonb_build_object('en',repeat('changed',1000)) WHERE id='dance'`,
	)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.PassEventDetail(t.Context(), "alice", "dance", chunk.NextCursor)
		require.ErrorIs(t, err, appclient.ErrReadStale)
	}
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET titles=jsonb_build_object('en',repeat('x',$1)) WHERE id='dance'`,
		core.ReadResourceBytes,
	)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.PassEventDetail(t.Context(), "alice", "dance", "")
		require.ErrorIs(t, err, appclient.ErrReadLimit)
	}
	_, err = db.Exec(
		t.Context(),
		`DELETE FROM core.pass_registration_announcements WHERE owner='alice'; DELETE FROM core.pass_bookings WHERE owner='alice'`,
	)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		owned, readErr := client.OwnsPassEvents(t.Context(), "alice", []string{"dance"})
		require.NoError(t, readErr)
		require.False(t, owned)
	}
}

func TestLocalRegistrationHistoryHTTPParity(t *testing.T) {
	t.Parallel()
	db, _, local, remote := registrationOperationsFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.order_proofs(id,owner,filename,body)
 SELECT md5(n::text)||md5(n::text),'alice','receipt.txt',convert_to('synthetic','UTF8') FROM generate_series(1,21)n;
 INSERT INTO core.pass_payment_attempts(id,event_id,submitter,proof_id,receiving_admin,received_at)
 SELECT id,'dance',owner,id,'bob','2026-01-01'::timestamptz FROM core.order_proofs;
 INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at) SELECT id,'alice',received_at FROM core.pass_payment_attempts;
 UPDATE core.pass_payment_admins SET hidden=true WHERE owner='bob';
 INSERT INTO core.pass_booking_admins(owner) VALUES('visitor');`)
	require.NoError(t, err)
	first, err := local.PassPaymentHistory(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, first.Items, core.ReadPageItems)
	require.True(t, first.More)
	other, err := remote.PassPaymentHistory(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Equal(t, other, first)
	last, err := local.PassPaymentHistory(t.Context(), "bob", "dance", first.NextCursor)
	require.NoError(t, err)
	require.Len(t, last.Items, 1)
	require.False(t, last.More)
	other, err = remote.PassPaymentHistory(t.Context(), "bob", "dance", first.NextCursor)
	require.NoError(t, err)
	require.Equal(t, other, last)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.PassPaymentHistory(t.Context(), "alice", "dance", "")
		requireCode(t, err, "forbidden")
		_, err = client.PassPaymentHistory(t.Context(), "visitor", "dance", "")
		requireCode(t, err, "forbidden")
		_, err = client.PassPaymentHistory(t.Context(), "bob", "other", "")
		requireCode(t, err, "forbidden")
		_, err = client.PassPaymentHistory(t.Context(), "bob", "other", first.NextCursor)
		requireCode(t, err, "read_cursor_invalid")
		_, err = client.PassPaymentHistory(t.Context(), "alice", "dance", first.NextCursor)
		requireCode(t, err, "read_cursor_invalid")
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		_, err = client.PassPaymentHistory(ctx, "bob", "dance", "")
		require.ErrorIs(t, err, context.Canceled)
	}
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='bob'`)
	require.NoError(t, err)
	for _, client := range []appclient.Client{local, remote} {
		_, err = client.PassPaymentHistory(t.Context(), "bob", "dance", first.NextCursor)
		requireCode(t, err, "forbidden")
	}
}
