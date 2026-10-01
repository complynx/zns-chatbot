package integration_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
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
	for _, completedBeforeSend := range []bool{false, true} {
		name := "delivered"
		if completedBeforeSend {
			name = "profile_completed_after_claim"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service := passbooking.Service{DB: f.db, Delivery: f.b.Delivery}
			_, err := f.db.Exec(t.Context(), `
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,partner,payment_admin,created_at,assigned_at,price)
 VALUES('dance','alice',1,'assigned','leader','couple','bob','bob',now(),now(),100),
 ('dance','bob',1,'assigned','follower','couple','alice','bob',now(),now(),100);
 UPDATE core.pass_events SET passport_required=true;
 INSERT INTO core.legacy_pass_import_references(source_key,bot_id,owner,source_kind,source_record_sha256,target_id,source_record)
 VALUES(repeat('b',64),77,'bob','user_marker',repeat('b',64),'marker','{"notified_passport_data_required":false}');
 INSERT INTO core.pass_passport_reminders(owner,legacy_source_key) VALUES('bob',repeat('b',64));`)
			require.NoError(t, err)
			count, err := service.ProcessPassportReminders(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, count)
			count, err = service.ProcessPassportReminders(t.Context())
			require.NoError(t, err)
			require.Zero(t, count)
			var noticeID int64
			var marked bool
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT n.id,r.notified_at IS NOT NULL
 FROM core.pass_notifications n JOIN core.pass_passport_reminders r ON r.owner=n.owner
 WHERE n.kind='passport_required' AND n.owner='alice'`).Scan(&noticeID, &marked))
			require.True(t, marked, "the atomic outbox suppression marker is not a transport receipt")
			queued, queuedErr := service.NotificationStatus(t.Context(), noticeID)
			require.NoError(t, queuedErr)
			require.Equal(t, "pending", queued.State)
			require.Zero(t, queued.MessageID, "enqueue must not claim successful transport")
			if completedBeforeSend {
				notices, pendingErr := service.PendingNotifications(t.Context())
				require.NoError(t, pendingErr)
				require.Len(t, notices, 1)
				require.Equal(t, noticeID, notices[0].ID)
				require.Equal(t, "alice", notices[0].Owner)
				require.True(t, notices[0].Current)
				_, err = f.db.Exec(
					t.Context(),
					`UPDATE core.pass_profiles SET passport='synthetic' WHERE owner='alice'`,
				)
				require.NoError(t, err)
				gate, beginErr := service.BeginNotification(
					t.Context(),
					passbooking.NotificationAttempt{Attempt: delivery.Attempt{
						ID: noticeID, Generation: notices[0].DeliveryAttempt,
					}, Wire: notificationTestWire()},
				)
				require.NoError(t, beginErr)
				require.False(t, gate.Ready)
				require.Equal(t, "notification_no_longer_current", gate.Reason)
				status, statusErr := service.NotificationStatus(t.Context(), noticeID)
				require.NoError(t, statusErr)
				require.Equal(t, "cancelled", status.State)
				require.Zero(t, status.MessageID)
				require.Empty(t, chatMessages(t, f, 101))
			} else {
				drainPassNotices(t, f)
				status, statusErr := service.NotificationStatus(t.Context(), noticeID)
				require.NoError(t, statusErr)
				require.Equal(t, "sent", status.State)
				require.Positive(t, status.MessageID)
				matches := 0
				for _, card := range chatMessages(t, f, 101) {
					if card.ID == status.MessageID {
						matches++
						require.Contains(t, strings.ToLower(card.Text), "passport")
					}
				}
				require.Equal(t, 1, matches, "receipt must identify a real delivered reminder")
				var passport string
				require.NoError(t, f.db.QueryRow(t.Context(),
					`SELECT passport FROM core.pass_profiles WHERE owner='alice'`).Scan(&passport))
				require.Empty(
					t,
					passport,
					"restart must prove marker deduplication while the profile is still eligible",
				)
			}
			before := chatMessages(t, f, 101)
			restarted := passbooking.Service{DB: f.db, Delivery: f.b.Delivery}
			count, err = restarted.ProcessPassportReminders(t.Context())
			require.NoError(t, err)
			require.Zero(t, count)
			drainPassNotices(t, f)
			require.Equal(t, before, chatMessages(t, f, 101), "startup replay must not duplicate a reminder")
			require.Empty(t, chatMessages(t, f, 202), "imported global marker must suppress Bob's reminder")
			var notices, markers int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.pass_notifications WHERE kind='passport_required'),
 (SELECT count(*) FROM core.pass_passport_reminders)`).Scan(&notices, &markers))
			require.Equal(t, 1, notices)
			require.Equal(t, 2, markers)
		})
	}
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
