package integration_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestOrderCapacityDeletionReleasesOwnBareSelectedClaims(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"unpaid", "cash"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			db := database(t)
			s := orders.Service{DB: db}
			o, err := s.Execute(t.Context(), "alice", orders.Command{
				EventID: "sandbox-festival",
				Name:    "create",
				Origin:  "manual",
				Key:     "create",
				Choice:  orderChoice("shuttle"),
			})
			require.NoError(t, err)
			if state == "cash" {
				cash := orderCommand("cash", o)
				cash.PaymentAdmin = "bob"
				o, err = s.Execute(t.Context(), "alice", cash)
				require.NoError(t, err)
			}
			_, err = db.Exec(
				t.Context(),
				`UPDATE core.order_capacity_slots SET reservation_id=$1,reserved_at='2026-01-01Z'
			WHERE seat=0 AND service IN ('shuttle','excursion_grodno_overview')`,
				o.ID,
			)
			require.NoError(t, err)
			_, err = db.Exec(
				t.Context(),
				`UPDATE core.order_capacity_slots SET reservation_id='another-in-flight',reserved_at='2026-01-01Z'
			WHERE seat=1 AND service='shuttle'`,
			)
			require.NoError(t, err)
			_, err = s.Execute(t.Context(), "alice", orderCommand("delete", o))
			require.NoError(t, err)
			var cleared bool
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT reservation_id IS NULL AND reservation_attempt_token IS NULL
			AND reservation_attempt_created_at IS NULL AND reserved_at IS NULL FROM core.order_capacity_slots
			WHERE service='shuttle' AND seat=0`).Scan(&cleared),
			)
			assert.True(
				t,
				cleared,
				"deletion releases the selected service even when its own claim has no payment token",
			)
			var untouched int
			require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_capacity_slots
			WHERE reservation_id='another-in-flight' OR (reservation_id=$1 AND service='excursion_grodno_overview')`, o.ID).Scan(&untouched))
			assert.Equal(t, 2, untouched, "other claims and services not selected by this order remain unchanged")
		})
	}
}

func TestOrderCapacitySeatStableReplayAndRelease(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	o, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("shuttle"),
		},
	)
	require.NoError(t, err)
	c := orderCommand("proof", o)
	c.ProofFile = uploadProof(t, s, "alice")
	var results [2]orders.Order
	var failures [2]error
	var workers sync.WaitGroup
	for i := range results {
		workers.Go(func() { results[i], failures[i] = s.Execute(t.Context(), "alice", c) })
	}
	workers.Wait()
	for _, failure := range failures {
		require.NoError(t, failure)
	}
	assert.Equal(t, results[0].Attempt, results[1].Attempt)
	var seat int
	var token string
	var reserved time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT seat,reservation_attempt_token,reserved_at FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&seat, &token, &reserved),
	)
	assert.Zero(t, seat)
	assert.Equal(t, results[0].Attempt, token)
	country := orderCommand("country", results[0])
	country.Country = "be"
	country.PaymentAdmin = "bob"
	o, err = s.Execute(t.Context(), "alice", country)
	require.NoError(t, err)
	var preserved time.Time
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reserved_at FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&preserved),
	)
	assert.Equal(t, reserved, preserved, "ordinary reconciliation must not rebuild the claim")
	_, err = s.Execute(t.Context(), "alice", orderCommand("cancel_proof", o))
	require.NoError(t, err)
	var occupied int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&occupied),
	)
	assert.Zero(t, occupied)
}

func TestOrderCapacityBareClaimsAndDeletedPaymentCleanup(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	_, err := db.Exec(t.Context(), `UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1');
	INSERT INTO core.order_capacity_slots(event_id,service,seat,reservation_id,reserved_at)
	VALUES('sandbox-festival','shuttle',0,'in-flight','2001-01-01Z')`)
	require.NoError(t, err)
	create := orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "create",
		Choice:  orderChoice("shuttle"),
	}
	_, err = s.Execute(t.Context(), "alice", create)
	requireCode(t, err, "sold_out")
	var reservation string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reservation_id FROM core.order_capacity_slots WHERE service='shuttle'`).
			Scan(&reservation),
	)
	assert.Equal(t, "in-flight", reservation, "bare claims have no invented timeout")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.order_capacity_slots SET reservation_attempt_token='deleted-payment' WHERE service='shuttle'`,
	)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", create)
	require.NoError(t, err, "missing persisted payment order releases tokened claim")
}

func TestOrderCapacityRollbackKeepsClaim(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	o, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("shuttle"),
		},
	)
	require.NoError(t, err)
	proof := orderCommand("proof", o)
	proof.ProofFile = uploadProof(t, s, "alice")
	o, err = s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`CREATE FUNCTION core.capacity_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic receipt failure'; END $$;
	CREATE TRIGGER capacity_test_fail BEFORE INSERT ON core.order_operations FOR EACH ROW EXECUTE FUNCTION core.capacity_test_fail()`,
	)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", orderCommand("cancel_proof", o))
	require.Error(t, err)
	var token string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reservation_attempt_token FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&token),
	)
	assert.Equal(t, o.Attempt, token)
	listed, err := s.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, o.Version, listed[0].Version)
	assert.Equal(t, "proof", listed[0].State)
}

func TestOrderCapacityMigrationPreservesExistingPaidAllocations(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	o, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("shuttle"),
		},
	)
	require.NoError(t, err)
	proof := orderCommand("proof", o)
	proof.ProofFile = uploadProof(t, s, "alice")
	o, err = s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	// Reconstruct the immediately preceding schema inside this disposable database.
	_, err = db.Exec(
		t.Context(),
		`DROP TABLE core.order_capacity_slots; DELETE FROM public.zns_schema_migrations WHERE name='047_order_capacity_reservations.sql'`,
	)
	require.NoError(t, err)
	require.NoError(t, store.Migrate(t.Context(), db))
	var token string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reservation_attempt_token FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&token),
	)
	assert.Equal(t, o.Attempt, token)
	listed, err := s.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, o, listed[0], "schema expansion must not rewrite choices, payment or versions")
}

func TestOrderCapacityAdoptsOwnBareSeat(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	o, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "create",
			Choice:  orderChoice("shuttle"),
		},
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.order_capacity_slots SET reservation_id=$1,reserved_at='2001-01-01Z' WHERE service='shuttle' AND seat=7`,
		o.ID,
	)
	require.NoError(t, err)
	proof := orderCommand("proof", o)
	proof.ProofFile = uploadProof(t, s, "alice")
	o, err = s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	var seat int
	var token string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT seat,reservation_attempt_token FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&seat, &token),
	)
	assert.Equal(t, 7, seat, "adoption must retain the source seat even when lower seats are free")
	assert.Equal(t, o.Attempt, token)
}

func TestOrderCapacityEarlierPaymentDisplacesLaterClaim(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	_, err := db.Exec(t.Context(), `UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1')`)
	require.NoError(t, err)
	create := orders.Command{
		EventID: "sandbox-festival",
		Name:    "create",
		Origin:  "manual",
		Key:     "create",
		Choice:  orderChoice("shuttle"),
	}
	early, err := s.Execute(t.Context(), "alice", create)
	require.NoError(t, err)
	late, err := s.Execute(t.Context(), "bob", create)
	require.NoError(t, err)
	// Model persisted source payment attempts arriving at reconciliation out of order.
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.orders SET state='proof',attempt='early',attempt_at='2026-01-01Z' WHERE id=$1`,
		early.ID,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.orders SET state='proof',attempt='late',attempt_at='2026-01-02Z' WHERE id=$1`,
		late.ID,
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.order_capacity_slots SET reservation_id=$1,reservation_attempt_token='late',reservation_attempt_created_at='2026-01-02Z',reserved_at='2026-01-02Z' WHERE service='shuttle' AND seat=0`,
		late.ID,
	)
	require.NoError(t, err)
	early.Attempt = "early"
	country := orderCommand("country", early)
	country.Country, country.PaymentAdmin = "be", "bob"
	_, err = s.Execute(t.Context(), "alice", country)
	require.NoError(t, err)
	var holder string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reservation_id FROM core.order_capacity_slots WHERE service='shuttle' AND seat=0`).
			Scan(&holder),
	)
	assert.Equal(t, early.ID, holder)
	listed, err := s.List(t.Context(), "bob", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.NotContains(t, listed[0].Choice.Extras, "shuttle")
	assert.Equal(t, "proof", listed[0].State)
	assert.Greater(t, listed[0].Version, late.Version)
}

func TestOrderCapacityMigrationRefusesOverbookedSource(t *testing.T) {
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
	_, err := s.Execute(t.Context(), "alice", create)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "bob", create)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `DROP TABLE core.order_capacity_slots;
	DELETE FROM public.zns_schema_migrations WHERE name='047_order_capacity_reservations.sql';
	UPDATE core.order_events SET extras=jsonb_set(extras,'{shuttle,capacity}','1');
	UPDATE core.orders SET state='paid'`)
	require.NoError(t, err)
	err = store.Migrate(t.Context(), db)
	require.ErrorContains(t, err, "order_capacity_existing_allocations_exceed_limit")
	var count int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE state='paid' AND choice->'extras' ? 'shuttle'`).
			Scan(&count),
	)
	assert.Equal(t, 2, count)
	var exists bool
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT to_regclass('core.order_capacity_slots') IS NOT NULL`).Scan(&exists),
	)
	assert.False(t, exists, "failed migration rolls back the entire schema expansion")
}

func TestOrderCapacityOldAttemptCannotAdoptOrReleaseNewerReservation(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	o, err := s.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "create", Choice: orderChoice("shuttle"),
	})
	require.NoError(t, err)
	proof := orderCommand("proof", o)
	proof.ProofFile = uploadProof(t, s, "alice")
	o, err = s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.order_capacity_slots SET reservation_attempt_token='newer-attempt',
	reservation_attempt_created_at=clock_timestamp()+interval '1 hour' WHERE reservation_id=$1`, o.ID)
	require.NoError(t, err)
	country := orderCommand("country", o)
	country.Country, country.PaymentAdmin = "be", "bob"
	o, err = s.Execute(t.Context(), "alice", country)
	require.NoError(t, err)
	assert.NotContains(t, o.Choice.Extras, "shuttle", "older payment cannot adopt the newer attempt's seat")
	_, err = s.Execute(t.Context(), "alice", orderCommand("cancel_proof", o))
	require.NoError(t, err)
	var token string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reservation_attempt_token FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&token),
	)
	assert.Equal(t, "newer-attempt", token)
}

func TestOrderCapacityCatalogCannotOrphanClaim(t *testing.T) {
	t.Parallel()
	db := database(t)
	s := orders.Service{DB: db}
	o, err := s.Execute(t.Context(), "alice", orders.Command{
		EventID: "sandbox-festival", Name: "create", Origin: "manual", Key: "create", Choice: orderChoice("shuttle"),
	})
	require.NoError(t, err)
	proof := orderCommand("proof", o)
	proof.ProofFile = uploadProof(t, s, "alice")
	o, err = s.Execute(t.Context(), "alice", proof)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.order_events SET extras=extras-'shuttle'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", orderCommand("cancel_proof", o))
	requireCode(t, err, "capacity_configuration_conflict")
	var token string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT reservation_attempt_token FROM core.order_capacity_slots WHERE reservation_id=$1`, o.ID).
			Scan(&token),
	)
	assert.Equal(t, o.Attempt, token)
	listed, err := s.List(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, o, listed[0])
}
