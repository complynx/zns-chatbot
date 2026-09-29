package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPassPaymentPagesRemainCompleteAfterReview(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book)
SELECT 'payer-'||n,4000+n,'Synthetic payer',true FROM generate_series(1,71) n;
INSERT INTO core.order_proofs(id,owner,filename,body)
SELECT md5(id)||md5(id),id,'receipt.txt',convert_to('synthetic','UTF8') FROM core.users WHERE id LIKE 'payer-%';
INSERT INTO core.pass_payment_attempts(id,event_id,submitter,proof_id,receiving_admin,received_at)
SELECT md5(owner)||md5(owner),'dance',owner,id,'bob',clock_timestamp() FROM core.order_proofs;
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,payment_attempt)
SELECT 'dance',submitter,1,'paid','leader','solo','bob',received_at,received_at,100,id FROM core.pass_payment_attempts;
INSERT INTO core.pass_payment_participants(attempt,owner,assigned_at)
SELECT payment_attempt,owner,assigned_at FROM core.pass_bookings;`)
	require.NoError(t, err)
	seen := map[string]bool{}
	cursor := ""
	for {
		page, pageErr := service.PaymentQueue(t.Context(), "bob", "dance", cursor)
		require.NoError(t, pageErr)
		require.LessOrEqual(t, len(page.Items), 25)
		for _, item := range page.Items {
			require.False(t, seen[item.Payment.Attempt])
			seen[item.Payment.Attempt] = true
		}
		if cursor == "" {
			require.Len(t, page.Items, 25)
			_, err = db.Exec(
				t.Context(),
				`UPDATE core.pass_payment_attempts SET decision='accepted',reviewed_by='bob',reviewed_at=clock_timestamp() WHERE id=$1`,
				page.Next,
			)
			require.NoError(t, err)
		}
		if page.Next == "" {
			break
		}
		cursor = page.Next
	}
	assert.Len(t, seen, 71)
	_, err = service.PaymentQueue(t.Context(), "alice", "dance", cursor)
	requireCode(t, err, "forbidden")
	_, err = db.Exec(t.Context(), `DELETE FROM core.pass_payment_admins WHERE event_id='dance' AND owner='bob'`)
	require.NoError(t, err)
	_, err = service.PaymentQueue(t.Context(), "bob", "dance", cursor)
	requireCode(t, err, "forbidden")
}
