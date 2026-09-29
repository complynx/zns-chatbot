package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestLegacyPassReceiptUnknownUploaderAndUnavailable(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	booking, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte("synthetic"))
	require.NoError(t, err)
	command := bookingCommand("proof", "submit", booking)
	command.ProofID = proof.ID
	booking, err = service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	payment, err := service.Payment(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	key := strings.Repeat("a", 64)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,source_kind,source_record_sha256,target_id,source_record) VALUES($1,77,'dance','proof',$1,$2,'{}');`,
		key,
		payment.Attempt,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_payment_attempts SET legacy_source_key=$1,submitter=NULL WHERE id=$2`,
		key,
		payment.Attempt,
	)
	require.NoError(t, err)
	payment, err = service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Nil(t, payment.Submitter)
	file, err := service.PaymentProof(t.Context(), "bob", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, []byte("synthetic"), file.Body)
	_, err = service.PaymentProof(t.Context(), "visitor", "dance", "alice")
	requireCode(t, err, "forbidden")
	queue, err := service.PaymentQueue(t.Context(), "bob", "dance", "")
	require.NoError(t, err)
	require.Len(t, queue.Items, 1)
	assert.Nil(t, queue.Items[0].Payment.Submitter)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_payment_attempts SET proof_id=NULL,proof_unavailable=true WHERE id=$1`,
		payment.Attempt,
	)
	require.NoError(t, err)
	_, err = service.PaymentProof(t.Context(), "alice", "dance", "alice")
	requireCode(t, err, "pass_receipt_unavailable")
	review := bookingCommand("proof_accept", "accept", passbooking.Booking{})
	review.Target = "alice"
	review.TargetVersion = booking.Version
	review.PaymentAttempt = payment.Attempt
	_, err = service.Execute(t.Context(), "bob", review)
	require.NoError(t, err)
	payment, err = service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	require.NotNil(t, payment.ReviewedBy)
	assert.Equal(t, "bob", *payment.ReviewedBy)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_payment_attempts SET legacy_source_key=NULL WHERE id=$1`,
		payment.Attempt,
	)
	require.Error(t, err, "ordinary attempts cannot have unknown submitter or missing proof")
}
func TestPassportStartupReminderGlobalAtomicMarker(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET passport_required=true; UPDATE core.pass_bookings SET state='assigned',assigned_at=now(),price=100;
 INSERT INTO core.legacy_pass_import_references(source_key,bot_id,owner,source_kind,source_record_sha256,target_id,source_record) VALUES(repeat('b',64),77,'bob','user_marker',repeat('b',64),'marker','{"notified_passport_data_required":false}');
 INSERT INTO core.pass_passport_reminders(owner,legacy_source_key) VALUES('bob',repeat('b',64));`,
	)
	require.NoError(t, err)
	count, err := service.ProcessPassportReminders(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	count, err = service.ProcessPassportReminders(t.Context())
	require.NoError(t, err)
	assert.Zero(t, count)
	notices, err := service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.Equal(t, "alice", notices[0].Owner)
	assert.True(t, notices[0].Current)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_profiles SET passport='synthetic' WHERE owner='alice'`)
	require.NoError(t, err)
	notices, err = service.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.False(t, notices[0].Current)
}
func TestImportedContactPreferenceUsesCurrentEligibleAdmin(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','visitor'); INSERT INTO core.pass_contact_preferences(event_id,owner,payment_admin) VALUES('dance','alice','bob')`,
	)
	require.NoError(t, err)
	booking, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	assert.Equal(t, "bob", booking.PaymentAdmin)
}

func TestLegacySharedReceiptPreservesParticipantActors(t *testing.T) {
	t.Parallel()
	for _, reviewed := range []bool{false, true} {
		name := "pending"
		if reviewed {
			name = "reviewed"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db, service := adminPairFixture(t)
			_, err := db.Exec(t.Context(), `
 UPDATE core.pass_bookings SET state='paid',assigned_at=now()-interval '2 hours',price=100;
 INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,source_kind,source_record_sha256,target_id,source_record) VALUES(repeat('d',64),77,'dance','proof',repeat('d',64),repeat('e',64),'{}');
 INSERT INTO core.pass_payment_attempts(id,event_id,submitter,receiving_admin,received_at,legacy_source_key,proof_unavailable) VALUES(repeat('e',64),'dance',NULL,NULL,now()-interval '1 hour',repeat('d',64),true);
 INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at) SELECT repeat('e',64),owner,assigned_at FROM core.pass_bookings;
 UPDATE core.pass_bookings SET payment_attempt=repeat('e',64);
 INSERT INTO core.legacy_pass_payment_metadata(event_id,owner,assigned_at,source_key,received_at,receiving_admin,proof_reference) SELECT event_id,owner,assigned_at,repeat('d',64),now()-interval '1 hour',owner,'receipt' FROM core.pass_bookings;`)
			require.NoError(t, err)
			if reviewed {
				_, err = db.Exec(
					t.Context(),
					`UPDATE core.pass_payment_attempts SET decision='accepted',reviewed_at=now(); UPDATE core.legacy_pass_payment_metadata SET accepted_at=now(),reviewed_by=owner;`,
				)
				require.NoError(t, err)
			}
			for _, owner := range []string{"alice", "bob"} {
				payment, readErr := service.Payment(t.Context(), "bob", "dance", owner)
				require.NoError(t, readErr)
				assert.Equal(t, owner, payment.ReceivingAdmin)
				if reviewed {
					require.NotNil(t, payment.ReviewedBy)
					assert.Equal(t, owner, *payment.ReviewedBy)
				} else {
					assert.Nil(t, payment.ReviewedBy)
				}
			}
			target, err := service.TakeoverTarget(t.Context(), "bob", "dance", 101)
			require.NoError(t, err)
			assert.Equal(t, "alice", target.ReceivingAdmin)
			body, err := service.Export(t.Context(), "bob")
			require.NoError(t, err)
			rows := exportRows(t, openExport(t, body), "Passes")
			assert.Equal(t, "101", rows[1][18])
			assert.Equal(t, "202", rows[2][18])
			queue, err := service.PaymentQueue(t.Context(), "bob", "dance", "")
			require.NoError(t, err)
			if reviewed {
				assert.Empty(t, queue.Items)
				return
			}
			require.Len(t, queue.Items, 1)
			assert.Equal(t, queue.Items[0].Owner, queue.Items[0].Payment.ReceivingAdmin)
			command := bookingCommand("proof_accept", "review", passbooking.Booking{Version: 1})
			command.Target = queue.Items[0].Owner
			command.TargetVersion = queue.Items[0].Payment.Version
			command.PaymentAttempt = queue.Items[0].Payment.Attempt
			_, err = service.Execute(t.Context(), "bob", command)
			require.NoError(t, err)
			for _, owner := range []string{"alice", "bob"} {
				payment, readErr := service.Payment(t.Context(), "bob", "dance", owner)
				require.NoError(t, readErr)
				require.NotNil(t, payment.ReviewedBy)
				assert.Equal(t, "bob", *payment.ReviewedBy)
				assert.Equal(t, owner, payment.ReceivingAdmin)
			}
		})
	}
}
func TestLegacySecondDeadlineMarkerSurvivesFirstReminder(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state='assigned',assigned_at=now()-interval '7 days',price=100;
 INSERT INTO core.pass_deadline_markers(event_id,owner,assigned_at,first_at,second_at) SELECT event_id,owner,assigned_at,NULL,now()-interval '1 day' FROM core.pass_bookings;`,
	)
	require.NoError(t, err)
	count, err := service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	assert.EqualValues(t, 2, count)
	var preserved int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_deadline_markers WHERE first_at IS NOT NULL AND second_at<first_at`).
			Scan(&preserved),
	)
	assert.Equal(t, 2, preserved)
}

func TestLegacyAssignmentTierExportIsIndependentAndGenerationBound(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state='assigned',assigned_at=now(),price=100,tier_index=0;
 INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,owner,source_kind,source_record_sha256,target_id,source_record) VALUES(repeat('f',64),77,'dance','alice','booking',repeat('f',64),'tier','{}');
 INSERT INTO core.legacy_pass_assignment_metadata(event_id,owner,assigned_at,source_key,assignment_tier_number) SELECT event_id,owner,assigned_at,repeat('f',64),7 FROM core.pass_bookings WHERE owner='alice';
 INSERT INTO core.legacy_pass_import_references(source_key,bot_id,event_id,owner,source_kind,source_record_sha256,target_id,source_record) VALUES(repeat('a',64),77,'dance','bob','booking',repeat('a',64),'missing-tier','{}');
 INSERT INTO core.legacy_pass_assignment_metadata(event_id,owner,assigned_at,source_key,assignment_tier_number) SELECT event_id,owner,assigned_at,repeat('a',64),NULL FROM core.pass_bookings WHERE owner='bob';`,
	)
	require.NoError(t, err)
	body, err := service.Export(t.Context(), "bob")
	require.NoError(t, err)
	rows := exportRows(t, openExport(t, body), "Passes")
	assert.Equal(t, "7", rows[1][14])
	assert.Empty(t, rows[2][14], "missing source assignment tier is not inferred from allocation tier")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET assigned_at=assigned_at+interval '1 day' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	body, err = service.Export(t.Context(), "bob")
	require.NoError(t, err)
	rows = exportRows(t, openExport(t, body), "Passes")
	assert.Equal(t, "1", rows[1][14], "new assignment must not inherit a prior source tier number")
}
