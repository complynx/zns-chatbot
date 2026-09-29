package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func adminPairFixture(t *testing.T) (*pgxpool.Pool, passbooking.Service) {
	t.Helper()
	db, service := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,partner,payment_admin,created_at)
 VALUES('dance','alice',1,'waitlist','leader','couple','bob','bob',now()),
 ('dance','bob',1,'waitlist','follower','couple','alice','bob',now())`,
	)
	require.NoError(t, err)
	return db, service
}

func TestPassAdminAssignmentCouplePricesAndAppend(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	price, tier := 101, 1
	kind, comment := "guest-pair", "synthetic invitation"
	command := passbooking.AdminAssignment{Event: "dance", Key: "force", Version: 1, Target: "alice", TargetVersion: 1,
		TotalPrice: &price, AppendTier: &tier, Kind: &kind, Comment: &comment}
	result, err := service.AdminAssign(t.Context(), "bob", command)
	require.NoError(t, err)
	require.Len(t, result.Bookings, 2)
	assert.Equal(t, 2, result.AssignedCount)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, 51, *alice.Price)
	assert.Equal(t, 50, *bob.Price)
	assert.Equal(t, "bob", alice.Partner)
	assert.Equal(t, "alice", bob.Partner)
	assert.Equal(t, kind, alice.Kind)
	assert.Equal(t, comment, bob.Comment)
	_, err = service.AdminAssign(t.Context(), "bob", command)
	require.NoError(t, err)
	var amount, ledger int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_admin_assignments`).Scan(&ledger))
	assert.Equal(t, 22, amount)
	assert.Equal(t, 1, ledger)
	changed := command
	changed.Comment = nil
	_, err = service.AdminAssign(t.Context(), "bob", changed)
	requireCode(t, err, "idempotency_conflict")
}

func TestPassAdminAssignmentActualPairedTierAndDateOverride(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET assignment_rule='paired';
 UPDATE core.pass_event_tiers SET amount=2;
 INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at,blocked_by_date) VALUES('dance',1,2,200,now()+interval '1 day',true);
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,tier_index)
 VALUES('dance','visitor',1,'assigned','leader','solo','bob',now(),now(),100,0)`)
	require.NoError(t, err)
	result, err := service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{Event: "dance", Key: "tiers", Version: 1, Target: "alice", TargetVersion: 1},
	)
	require.NoError(t, err)
	require.Len(t, result.Bookings, 2)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, 200, *alice.Price)
	assert.Equal(t, 100, *bob.Price)
	assert.Equal(t, 1, *alice.TierIndex)
	assert.Zero(t, *bob.TierIndex)
}

func TestPassAdminAssignmentFreeSplitsPair(t *testing.T) {
	t.Parallel()
	_, service := adminPairFixture(t)
	price := 1
	include := false
	_, err := service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "partial-free",
			Version:       1,
			Target:        "alice",
			TargetVersion: 1,
			TotalPrice:    &price,
			SkipBalance:   &include,
		},
	)
	require.NoError(t, err)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, 1, *alice.Price)
	assert.Zero(t, *bob.Price)
	assert.Equal(t, "assigned", alice.State)
	assert.Equal(t, "paid", bob.State)
	assert.Empty(t, alice.Partner)
	assert.Empty(t, bob.Partner)
	assert.Equal(t, "solo", alice.Kind)
	assert.Equal(t, "solo", bob.Kind)
	require.NotNil(t, bob.SkipBalance)
	assert.False(t, *bob.SkipBalance, "explicit false includes even a free pass")
	payment, err := service.Payment(t.Context(), "bob", "dance", "bob")
	require.NoError(t, err)
	assert.Equal(t, "free", payment.Kind)
	assert.Empty(t, payment.ProofID)
	assert.Equal(t, "accepted", payment.Decision)
	require.NotNil(t, payment.ReviewedBy)
	assert.Equal(t, "bob", *payment.ReviewedBy)
	_, err = service.PaymentProof(t.Context(), "bob", "dance", "bob")
	requireCode(t, err, "forbidden")
}

func TestPassAdminAssignmentPreservesReceiptOrReplacesIt(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	total := 201
	_, err := service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "assign",
			Version:       1,
			Target:        "alice",
			TargetVersion: 1,
			TotalPrice:    &total,
		},
	)
	require.NoError(t, err)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	proof, err := (orders.Service{DB: db}).UploadProof(t.Context(), "alice", "receipt.txt", []byte("synthetic receipt"))
	require.NoError(t, err)
	submit := bookingCommand("proof", "proof", alice)
	submit.ProofID = proof.ID
	alice, err = service.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	bob, err := service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	old, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	comment := "metadata only"
	_, err = service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "metadata",
			Version:       bob.Version,
			Target:        "alice",
			TargetVersion: alice.Version,
			Comment:       &comment,
		},
	)
	require.NoError(t, err)
	next, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, alice.AssignedAt, next.AssignedAt)
	assert.Equal(t, alice.Price, next.Price)
	assert.Equal(t, "paid", next.State)
	assert.Empty(t, next.Partner)
	bob, err = service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, 100, *bob.Price, "survivor retains individual price")
	accept := bookingCommand("proof_accept", "review-after-metadata", bob)
	accept.Target, accept.TargetVersion, accept.PaymentAttempt = "alice", next.Version, old.Attempt
	_, err = service.Execute(t.Context(), "bob", accept)
	require.NoError(t, err, "metadata-only reassignment must not invalidate pending receipt")
	next, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err = service.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	replacement := 75
	_, err = service.AdminAssign(
		t.Context(),
		"bob",
		passbooking.AdminAssignment{
			Event:         "dance",
			Key:           "reprice",
			Version:       bob.Version,
			Target:        "alice",
			TargetVersion: next.Version,
			TotalPrice:    &replacement,
		},
	)
	require.NoError(t, err)
	_, err = service.Payment(t.Context(), "alice", "dance", "alice")
	requireCode(t, err, "pass_payment_missing")
	surviving, err := service.Payment(t.Context(), "bob", "dance", "bob")
	require.NoError(t, err)
	assert.Equal(t, old.Attempt, surviving.Attempt)
	next, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", next.State)
	assert.Equal(t, 75, *next.Price)
	assert.NotEqual(t, alice.AssignedAt, next.AssignedAt)
	var history int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&history))
	assert.Equal(t, 1, history, "detachment preserves the immutable receipt attempt")
	free := 0
	_, err = service.AdminAssign(t.Context(), "bob", passbooking.AdminAssignment{
		Event: "dance", Key: "replace-with-free", Version: bob.Version, Target: "alice",
		TargetVersion: next.Version, TotalPrice: &free,
	})
	require.NoError(t, err)
	freePayment, err := service.Payment(t.Context(), "alice", "dance", "alice")
	require.NoError(t, err)
	assert.Equal(t, "free", freePayment.Kind)
	assert.Equal(t, "accepted", freePayment.Decision)
	assert.NotEqual(t, old.Attempt, freePayment.Attempt)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&history))
	assert.Equal(t, 2, history)
}
