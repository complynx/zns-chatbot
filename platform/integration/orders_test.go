package integration_test

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func orderChoice(extra string) *orders.ChoiceInput {
	return &orders.ChoiceInput{Customer: "Test User", Extras: map[string]json.RawMessage{extra: json.RawMessage(`0`)}}
}

func orderCommand(name string, o orders.Order) orders.Command {
	return orders.Command{EventID: "sandbox-festival", OrderID: o.ID, Version: o.Version, Name: name,
		Attempt: o.Attempt, Origin: "manual", Key: name + o.ID + time.Now().Format(time.RFC3339Nano)}
}

func TestProofCountryRoutingRemainsAvailableAfterDeadline(t *testing.T) {
	t.Parallel()
	db := database(t)
	service := orders.Service{DB: db}
	order, err := service.Execute(t.Context(), "alice", orders.Command{EventID: "sandbox-festival", Name: "create",
		Origin: "manual", Key: "create", Choice: orderChoice("preparty")})
	require.NoError(t, err)
	proof := orderCommand("proof", order)
	proof.ProofFile = uploadProof(t, service, "alice")
	order, err = service.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.order_events SET deadline=clock_timestamp()-interval '1 second'`)
	require.NoError(t, err)
	country := orderCommand("country", order)
	country.Country, country.PaymentAdmin = "be", "bob"
	_, err = service.Execute(t.Context(), "bob", country)
	requireCode(t, err, "order_not_found")
	order, err = service.Execute(t.Context(), "alice", country)
	require.NoError(t, err)
	assert.Equal(t, "be", order.Country)
	assert.Equal(t, "proof", order.State)
	_, err = service.Execute(t.Context(), "alice", orderCommand("cancel_proof", order))
	requireCode(t, err, "deadline")
	_, err = service.Execute(t.Context(), "bob", orderCommand("accept", order))
	require.NoError(t, err)
}

func TestOrderReconciliationPreservesHistoricalPrices(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	create := orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "create",
		Choice:  orderChoice("shuttle"),
	}
	alice, err := s.Execute(t.Context(), "alice", create)
	require.NoError(t, err)
	create.Choice.Extras["preparty"] = json.RawMessage(`0`)
	_, err = s.Execute(t.Context(), "bob", create)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.order_events SET extras=jsonb_set(jsonb_set(extras,'{shuttle,capacity}','1'),'{shuttle,price}','70')`,
	)
	require.NoError(t, err)
	proof := orderCommand("proof", alice)
	proof.ProofFile = uploadProof(t, s, "alice")
	_, err = s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	listed, err := s.List(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotContains(t, listed[0].Choice.Extras, "shuttle")
	assert.Equal(t, orders.Money(3500), listed[0].Choice.Extras["total"])
	assert.Equal(t, orders.Money(3500), listed[0].Choice.Total)
	var oldTotal, newTotal int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT old_total,new_total FROM core.order_notices WHERE owner='bob'`).
			Scan(&oldTotal, &newTotal),
	)
	assert.EqualValues(t, 10000, oldTotal)
	assert.EqualValues(t, 3500, newTotal)
	notices, err := s.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 1)
	assert.Equal(t, "bob", notices[0].Recipient)
	assert.Equal(t, "capacity", notices[0].Kind)
	assert.Equal(t, []string{"shuttle"}, notices[0].Removed)
	assert.Equal(t, orders.Money(3500), notices[0].Total)
}

func TestOrderPaymentCapacityAndIsolation(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	_, err := db.Exec(t.Context(), `UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1');
	INSERT INTO core.order_admins(event_id,owner,country) VALUES('sandbox-festival','bob','be') ON CONFLICT DO NOTHING`)
	require.NoError(t, err)
	create := orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "a",
		Choice:  orderChoice("shuttle"),
	}
	alice, err := s.Execute(t.Context(), "alice", create)
	require.NoError(t, err)
	bob, err := s.Execute(t.Context(), "bob", create)
	require.NoError(t, err)
	assert.Equal(t, orders.Money(6500), alice.Choice.Total, "server price wins")
	_, err = s.Execute(t.Context(), "visitor", create)
	requireCode(t, err, "forbidden")
	_, err = s.Execute(t.Context(), "bob", orderCommand("delete", alice))
	requireCode(t, err, "order_not_found")

	proof := orderCommand("proof", alice)
	proof.ProofFile = uploadProof(t, s, "alice")
	paid, err := s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	assert.Equal(t, "proof", paid.State)
	assert.NotEmpty(t, paid.Attempt)
	listed, err := s.List(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotContains(t, listed[0].Choice.Extras, "shuttle", "pending proof already holds the seat")
	assert.Greater(t, listed[0].Version, bob.Version)
	assert.Zero(t, listed[0].Choice.Total)
	replay, err := s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	paidJSON, err := json.Marshal(paid)
	require.NoError(t, err)
	replayJSON, err := json.Marshal(replay)
	require.NoError(t, err)
	assert.JSONEq(t, string(paidJSON), string(replayJSON), "replay preserves the API result")
	proof.ProofFile = "changed.pdf"
	_, err = s.Execute(t.Context(), "alice", proof)
	requireCode(t, err, "key_conflict")
	_, err = s.Execute(t.Context(), "alice", orderCommand("accept", paid))
	requireCode(t, err, "forbidden")
	stale := orderCommand("accept", paid)
	stale.Attempt = "old"
	_, err = s.Execute(t.Context(), "bob", stale)
	requireCode(t, err, "stale_attempt")
	accepted, err := s.Execute(t.Context(), "bob", orderCommand("accept", paid))
	require.NoError(t, err)
	assert.Equal(t, "paid", accepted.State)
	acceptedCommand := orderCommand("accept", paid)
	// A fresh key cannot repeat a completed payment transition.
	_, err = s.Execute(t.Context(), "bob", acceptedCommand)
	requireCode(t, err, "stale_version")
	_, err = s.Execute(t.Context(), "alice", orderCommand("cancel_proof", accepted))
	requireCode(t, err, "payment_locked")
	var notices int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_notices`).Scan(&notices))
	assert.Equal(t, 1, notices)
}

func TestOrderCashEditInvalidatesAttemptAndDoesNotReserve(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.order_admins(event_id,owner,country) VALUES('sandbox-festival','bob','be') ON CONFLICT DO NOTHING`,
	)
	require.NoError(t, err)
	create := orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "agent",
		Key:     "a",
		Choice:  orderChoice("shuttle"),
	}
	o, err := s.Execute(t.Context(), "alice", create)
	require.NoError(t, err)
	cash := orderCommand("cash", o)
	cash.PaymentAdmin = "bob"
	pending, err := s.Execute(t.Context(), "alice", cash)
	require.NoError(t, err)
	edit := orderCommand("edit", pending)
	edit.Choice = orderChoice("preparty")
	edited, err := s.Execute(t.Context(), "alice", edit)
	require.NoError(t, err)
	assert.Empty(t, edited.Attempt)
	assert.Empty(t, edited.PaymentAdmin)
	assert.Equal(t, "unpaid", edited.State)
	accept := orderCommand("accept", edited)
	accept.Attempt = pending.Attempt
	_, err = s.Execute(t.Context(), "bob", accept)
	requireCode(t, err, "stale_attempt")
	proof := orderCommand("proof", edited)
	proof.Origin = "agent"
	proof.ProofFile = "fake.pdf"
	_, err = s.Execute(t.Context(), "alice", proof)
	requireCode(t, err, "invalid_proof")
	_, err = db.Exec(t.Context(), `UPDATE core.order_events SET deadline=now()-interval '1 second'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", orderCommand("delete", edited))
	requireCode(t, err, "deadline")
}

func TestOrderConcurrentProofCannotOversell(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	_, err := db.Exec(t.Context(), `UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1')`)
	require.NoError(t, err)
	var all []orders.Order
	for _, owner := range []string{"alice", "bob"} {
		o, createError := s.Execute(
			t.Context(),
			owner,
			orders.Command{
				EventID: "sandbox-festival",
				Name:    "create",
				Origin:  "manual",
				Key:     "create",
				Choice:  orderChoice("shuttle"),
			},
		)
		require.NoError(t, createError)
		all = append(all, o)
	}
	var group sync.WaitGroup
	results := make(chan error, len(all))
	for _, o := range all {
		proofID := uploadProof(t, s, o.Owner)
		group.Go(func() {
			c := orderCommand("proof", o)
			c.ProofFile = proofID
			_, proofError := s.Execute(t.Context(), o.Owner, c)
			results <- proofError
		})
	}
	group.Wait()
	close(results)
	for err := range results {
		if err != nil {
			requireCode(t, err, "stale_version")
		}
	}
	var seats int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE state IN ('proof','paid') AND choice->'extras' ? 'shuttle'`).
			Scan(&seats),
	)
	assert.Equal(t, 1, seats)
}

func TestMigrationReplayAndChecksum(t *testing.T) {
	t.Parallel()
	db := database(t)
	require.NoError(t, store.Migrate(t.Context(), db))
	_, err := db.Exec(
		t.Context(),
		`UPDATE public.zns_schema_migrations SET checksum='tampered' WHERE name='001_bootstrap.sql'`,
	)
	require.NoError(t, err)
	assert.ErrorContains(t, store.Migrate(t.Context(), db), "checksum mismatch")
}

func uploadProof(t *testing.T, s orders.Service, owner string) string {
	t.Helper()
	proof, err := s.UploadProof(t.Context(), owner, "receipt.pdf", []byte("%PDF-test receipt"))
	require.NoError(t, err)
	return proof.ID
}
