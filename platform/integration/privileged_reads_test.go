package integration_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func seedPrivilegedReads(t *testing.T, f *fixture) {
	t.Helper()
	seedScriptDomains(t, f)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_admins(event_id,owner,hidden) VALUES('script-dance','alice',true);
 INSERT INTO core.massage_specialists(event_id,owner,name) VALUES('sandbox-festival','alice','Private practitioner');
 INSERT INTO core.massage_work(event_id,specialist,starts_at,ends_at)
 SELECT 'sandbox-festival','alice',now()+n*interval '1 day',now()+n*interval '1 day'+interval '2 hours' FROM generate_series(1,25)n;
 INSERT INTO core.massage_bookings(id,event_id,party_id,owner,specialist,slot,length,starts_at,ends_at,price,price_rub)
 SELECT 'privileged-'||n,'sandbox-festival','script-night','visitor','alice',n,1,now()+n*interval '1 day',now()+n*interval '1 day'+interval '15 minutes',10,10 FROM generate_series(1,25)n;
 INSERT INTO core.pass_payment_attempts(id,event_id,submitter,kind,receiving_admin,received_at,decision,reviewed_by,reviewed_at)
 SELECT lpad(n::text,64,'0'),'script-dance','visitor','free','bob',now()+n*interval '1 second','accepted','bob',now()+n*interval '1 second' FROM generate_series(1,25)n;
 INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at) SELECT id,'visitor',received_at-interval '1 day' FROM core.pass_payment_attempts;
 UPDATE core.users SET can_book=false WHERE id='alice'`,
	)
	require.NoError(t, err)
}

func TestPrivilegedReadPagesAndCurrentScopes(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPrivilegedReads(t, f)
	capabilities, err := (core.Service{DB: f.db}).PrivilegedReads(t.Context(), "alice")
	require.NoError(t, err)
	assert.True(t, capabilities.PaymentReads)
	assert.True(t, capabilities.PractitionerReads)
	massageService := massage.Service{DB: f.db}
	cursor := ""
	seen := map[int64]bool{}
	for {
		page, readErr := massageService.PractitionerSchedule(t.Context(), "alice", "sandbox-festival", cursor)
		require.NoError(t, readErr)
		require.LessOrEqual(t, len(page.Items), 20)
		for _, item := range page.Items {
			assert.False(t, seen[item.ID])
			seen[item.ID] = true
		}
		if cursor == "" {
			_, readErr = massageService.PractitionerSchedule(t.Context(), "bob", "sandbox-festival", page.NextCursor)
			requireCode(t, readErr, "read_cursor_invalid")
		}
		if !page.More {
			break
		}
		require.NotEmpty(t, page.NextCursor)
		cursor = page.NextCursor
	}
	assert.Len(t, seen, 25)
	bookings, err := massageService.PractitionerBookings(t.Context(), "alice", "sandbox-festival", "", "")
	require.NoError(t, err)
	require.Len(t, bookings.Items, 20)
	assert.True(t, bookings.More)
	for _, item := range bookings.Items {
		assert.Equal(t, "alice", item.Specialist)
		assert.NotEqual(t, "alice", item.Owner)
	}
	rest, err := massageService.PractitionerBookings(t.Context(), "alice", "sandbox-festival", "", bookings.NextCursor)
	require.NoError(t, err)
	assert.Len(t, rest.Items, 5)
	assert.False(t, rest.More)
	_, err = massageService.PractitionerBookings(
		t.Context(),
		"alice",
		"sandbox-festival",
		"script-night",
		bookings.NextCursor,
	)
	requireCode(t, err, "read_cursor_invalid")
	_, err = massageService.PractitionerBookings(t.Context(), "visitor", "sandbox-festival", "", "")
	requireCode(t, err, "forbidden")
	_, err = f.db.Exec(t.Context(), `UPDATE core.massage_bookings SET cancelled_at=now() WHERE id='privileged-1'`)
	require.NoError(t, err)
	active, err := massageService.PractitionerBookings(t.Context(), "alice", "sandbox-festival", "", "")
	require.NoError(t, err)
	assert.NotEqual(t, "privileged-1", active.Items[0].ID)
	history, err := (passbooking.Service{DB: f.db}).PaymentHistoryPage(t.Context(), "alice", "script-dance", "")
	require.NoError(t, err)
	require.Len(t, history.Items, 20)
	assert.True(t, history.More)
	tail, err := (passbooking.Service{DB: f.db}).PaymentHistoryPage(
		t.Context(),
		"alice",
		"script-dance",
		history.NextCursor,
	)
	require.NoError(t, err)
	assert.Len(t, tail.Items, 5)
	assert.False(t, tail.More)
	assert.Equal(t, "visitor", history.Items[0].Participant, "historical participant has no current booking")
	_, err = f.db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE owner='alice'`)
	require.NoError(t, err)
	_, err = (passbooking.Service{DB: f.db}).PaymentHistoryPage(
		t.Context(),
		"alice",
		"script-dance",
		history.NextCursor,
	)
	requireCode(t, err, "forbidden")
	scopes, err := (core.Service{DB: f.db}).PrivilegedReadEvents(t.Context(), "alice", "")
	require.NoError(t, err)
	require.Len(t, scopes.Items, 1)
	assert.False(t, scopes.Items[0].PaymentReads)
	assert.True(t, scopes.Items[0].PractitionerReads)
}

func TestPrivilegedReadEventNavigationIncludesHistoricalAndMassageOnly(t *testing.T) {
	t.Parallel()
	f := setup(t)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) SELECT 'past-'||lpad(n::text,2,'0'),now()-interval '1 day' FROM generate_series(1,25)n;
 INSERT INTO core.pass_payment_admins(event_id,owner) SELECT id,'alice' FROM core.pass_events;
 INSERT INTO core.massage_events(id) VALUES('massage-only'); INSERT INTO core.massage_specialists(event_id,owner,name) VALUES('massage-only','alice','Me')`,
	)
	require.NoError(t, err)
	service := core.Service{DB: f.db}
	cursor := ""
	seen := map[string]bool{}
	for {
		page, readErr := service.PrivilegedReadEvents(t.Context(), "alice", cursor)
		require.NoError(t, readErr)
		for _, item := range page.Items {
			assert.False(t, seen[item.Event])
			seen[item.Event] = true
		}
		if !page.More {
			break
		}
		cursor = page.NextCursor
	}
	assert.Len(t, seen, 26)
	assert.True(t, seen["massage-only"])
	assert.True(t, seen["past-25"])
	_, err = service.PrivilegedReadEvents(t.Context(), "visitor", "")
	requireCode(t, err, "forbidden")
	for _, path := range []string{"/v1/privileged-read-events", "/v1/passes/events/past-01/payment-history", "/v1/massage/practitioner/schedule?event=massage-only"} {
		code, _ := domainHTTP(t, f, "", path)
		assert.Equal(t, 401, code)
		code, _ = domainHTTP(t, f, "visitor", path)
		assert.Equal(t, 403, code)
	}
}

func TestPrivilegedReadHistorySharedParticipantSnapshots(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPrivilegedReads(t, f)
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at) VALUES(lpad('1',64,'0'),'bob',now()-interval '2 days');
 INSERT INTO core.order_proofs(id,owner,filename,body) VALUES('history-proof','visitor','private-proof.txt','secret');
 UPDATE core.pass_payment_attempts SET kind='receipt',proof_id='history-proof',decision='rejected' WHERE id=lpad('1',64,'0')`,
	)
	require.NoError(t, err)
	page, err := (passbooking.Service{DB: f.db}).PaymentHistoryPage(t.Context(), "alice", "script-dance", "")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(page.Items), 2)
	assert.Equal(t, page.Items[0].Attempt, page.Items[1].Attempt)
	assert.NotEqual(t, page.Items[0].Participant, page.Items[1].Participant)
	assert.Equal(t, "rejected", page.Items[0].Decision)
	assert.NotEqual(t, fmt.Sprint(page.Items[0].AssignedAt), fmt.Sprint(page.Items[1].AssignedAt))
}

func TestPrivilegedPaymentHistoryPreservesLegacyParticipantActors(t *testing.T) {
	t.Parallel()
	f := setup(t)
	seedPrivilegedReads(t, f)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at)
 SELECT id,'bob',received_at-interval '2 days' FROM core.pass_payment_attempts WHERE id=lpad('1',64,'0');
 INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,owner,source_kind,source_record_sha256,target_id,source_record)
 VALUES(repeat('a',64),1,'script-dance','bob','proof',repeat('b',64),lpad('1',64,'0'),'{}');
 UPDATE core.pass_payment_attempts SET kind='receipt',proof_id=NULL,proof_unavailable=true,legacy_source_key=repeat('a',64),receiving_admin=NULL,reviewed_by=NULL WHERE id=lpad('1',64,'0');
 INSERT INTO core.legacy_pass_payment_metadata(event_id,owner,assigned_at,source_key,received_at,receiving_admin,reviewed_by)
 SELECT p.event_id,m.owner,m.assigned_at,repeat('a',64),p.received_at,'alice','visitor' FROM core.pass_payment_participants m
 JOIN core.pass_payment_attempts p ON p.id=m.attempt WHERE m.attempt=lpad('1',64,'0') AND m.owner='bob';
 UPDATE core.pass_bookings SET assigned_at=now() WHERE owner='bob'`)
	require.NoError(t, err)
	service := passbooking.Service{DB: f.db}
	page, err := service.PaymentHistoryPage(t.Context(), "alice", "script-dance", "")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(page.Items), 2)
	assert.Equal(t, "bob", page.Items[0].Participant)
	assert.Equal(
		t,
		"alice",
		page.Items[0].ReceivingAdmin,
		"metadata follows participant snapshot, not current assignment",
	)
	require.NotNil(t, page.Items[0].ReviewedBy)
	assert.Equal(t, "visitor", *page.Items[0].ReviewedBy)
	assert.Empty(t, page.Items[1].ReceivingAdmin, "never invent unknown participant receiver from current contact")
	assert.Nil(t, page.Items[1].ReviewedBy)
	assert.True(t, page.Items[0].ProofUnavailable)
	assert.False(t, page.Items[0].ProofAvailable)
	_, err = f.db.Exec(t.Context(), `UPDATE core.pass_payment_attempts SET reviewed_by='bob' WHERE id=lpad('1',64,'0')`)
	require.NoError(t, err)
	current, err := service.PaymentHistoryPage(t.Context(), "alice", "script-dance", "")
	require.NoError(t, err)
	require.NotNil(t, current.Items[0].ReviewedBy)
	assert.Equal(t, "bob", *current.Items[0].ReviewedBy)
}
