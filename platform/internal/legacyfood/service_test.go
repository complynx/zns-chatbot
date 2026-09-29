package legacyfood_test

import (
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func foodDatabase(t *testing.T) legacyfood.Service {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL required")
		}
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "synthetic_qa_zns_food_test_" + strings.ToLower(rand.Text())
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted)
	require.NoError(t, err)
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.ConnConfig.Database = name
	db, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, dropErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, dropErr)
	})
	require.NoError(t, store.Migrate(t.Context(), db))
	require.NoError(t, store.Seed(t.Context(), db))
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_events(id,finishes_at) VALUES('food-event','2035-01-01Z');
 INSERT INTO core.food_events(event_id,bot_id,menu,menu_sha256,meal_prices,activity_prices,deadline,cacao_capacity,first_before,last_before,notify_after)
 VALUES('food-event',77,'{"friday":{"lunch":[{"title_ru":"Суп","title_en":"Soup","price":185}]}}',repeat('a',64),
 '{"with_soup":665,"without_soup":555}','{"party":2000,"party_and_classes":2500,"all_classes":2000,"yoga":750,"cacao":1000,"soundhealing":1000}',
 '2035-01-01Z',1,'7 days','1 day','1 hour');
 INSERT INTO core.food_admins(event_id,owner,can_export,can_review,can_assign) VALUES('food-event','bob',true,true,true)`)
	require.NoError(t, err)
	return legacyfood.Service{
		DB:    db,
		BotID: 77,
		Delivery: delivery.Settings{
			BotID:        909090,
			BotInterval:  time.Millisecond,
			ChatInterval: time.Millisecond,
			Fallback:     30 * time.Second,
		},
	}
}

func command(order legacyfood.Order, name, kind string) legacyfood.Command {
	return legacyfood.Command{
		EventID: "food-event",
		OrderID: order.ID,
		Version: order.Version,
		Name:    name,
		Kind:    kind,
		Key:     rand.Text(),
	}
}

func mealOrder(t *testing.T, s legacyfood.Service, owner string) legacyfood.Order {
	t.Helper()
	c := command(legacyfood.Order{}, "save_meals", "")
	c.Meals = legacyfood.MealSelection{
		"friday": {Lunch: &legacyfood.LunchSelection{Type: "individual-items", Items: json.RawMessage(`[0]`)}},
	}
	order, err := s.Execute(t.Context(), owner, c)
	require.NoError(t, err)
	return order
}

func TestIndependentPaymentsAndReplacementHistory(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	order := mealOrder(t, s, "alice")
	toggle := command(order, "toggle_activity", "")
	toggle.Activity = "open"
	order, err := s.Execute(t.Context(), "alice", toggle)
	require.NoError(t, err)
	order, err = s.Execute(t.Context(), "alice", command(order, "begin_payment", legacyfood.Meals))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: s.DB}).UploadProof(t.Context(), "alice", "meals.pdf", []byte("meal receipt"))
	require.NoError(t, err)
	submit := command(order, "submit_proof", legacyfood.Meals)
	submit.ProofID = proof.ID
	order, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Submitted, order.MealPayment.Status)
	assert.Equal(t, legacyfood.Pending, order.ActivityPayment.Status)
	toggle = command(order, "toggle_activity", "")
	toggle.Activity = "yoga"
	order, err = s.Execute(t.Context(), "alice", toggle)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", command(order, "delete_meals", ""))
	require.Error(t, err)
	reject := command(order, "reject", legacyfood.Meals)
	reject.Generation = order.MealPayment.Generation
	_, err = s.Execute(t.Context(), "alice", reject)
	require.Error(t, err)
	order, err = s.Execute(t.Context(), "bob", reject)
	require.NoError(t, err)
	replayed, err := s.Execute(t.Context(), "bob", reject)
	require.NoError(t, err)
	assert.Equal(t, order.Version, replayed.Version)
	second, err := (orders.Service{DB: s.DB}).UploadProof(
		t.Context(),
		"alice",
		"replacement.pdf",
		[]byte("new receipt"),
	)
	require.NoError(t, err)
	submit = command(order, "submit_proof", legacyfood.Meals)
	submit.Generation = order.MealPayment.Generation
	submit.ProofID = second.ID
	order, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	assert.Equal(t, int64(2), order.MealPayment.Generation)
	var previousStatus, previousProof string
	require.NoError(
		t,
		s.DB.QueryRow(t.Context(), `SELECT status,proof_id FROM core.food_payments WHERE order_id=$1 AND kind='meals' AND generation=1`, order.ID).
			Scan(&previousStatus, &previousProof),
	)
	assert.Equal(t, legacyfood.Rejected, previousStatus)
	assert.Equal(t, proof.ID, previousProof)
	_, err = s.Execute(t.Context(), "bob", command(order, "accept", legacyfood.Meals))
	require.Error(t, err)
}

func TestCacaoCountsUnpaidChoicesAtomically(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	first := mealOrder(t, s, "alice")
	second := mealOrder(t, s, "bob")
	commands := []legacyfood.Command{command(first, "toggle_activity", ""), command(second, "toggle_activity", "")}
	for index := range commands {
		commands[index].Activity = "cacao"
	}
	var workers sync.WaitGroup
	var results [2]legacyfood.Order
	var failures [2]error
	for index, owner := range []string{"alice", "bob"} {
		workers.Go(func() { results[index], failures[index] = s.Execute(t.Context(), owner, commands[index]) })
	}
	workers.Wait()
	selected := 0
	for index, result := range results {
		require.NoError(t, failures[index])
		if result.Activities["cacao"] {
			selected++
		}
		assert.Equal(t, legacyfood.Pending, result.ActivityPayment.Status)
	}
	assert.Equal(t, 1, selected)
}

func TestReminderMarkersAndNoOrderBranch(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	order := mealOrder(t, s, "alice")
	_, err := s.DB.Exec(t.Context(), `UPDATE core.food_events SET deadline=clock_timestamp()+interval '2 hours';
 UPDATE core.food_orders SET last_updated=clock_timestamp()-interval '2 hours';
 INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,tier_index)
 VALUES('food-event','bob',1,'paid','leader','solo','bob',now(),now(),0,0)`)
	require.NoError(t, err)
	require.NoError(t, s.QueueReminders(t.Context()))
	require.NoError(t, s.QueueReminders(t.Context()))
	notices, err := s.PendingNotifications(t.Context())
	require.NoError(t, err)
	require.Len(t, notices, 2)
	for _, notice := range notices {
		require.NoError(
			t,
			s.CompleteNotification(
				t.Context(),
				legacyfood.NotificationCompletion{
					ID:      notice.ID,
					Attempt: notice.DeliveryAttempt,
					Outcome: delivery.Outcome{Kind: delivery.Cancelled, Reason: "synthetic_domain_notice_consumed"},
				},
			),
		)
	}
	_, err = s.DB.Exec(
		t.Context(),
		`INSERT INTO core.legacy_food_import_references(source_key,bot_id,event_id,source_kind,source_record_sha256,target_id,source_record)
	 VALUES(repeat('b',64),77,'food-event','food',repeat('c',64),$1,'{}')`,
		order.ID,
	)
	require.NoError(t, err)
	_, err = s.DB.Exec(
		t.Context(),
		`UPDATE core.food_notifications SET sent_at=NULL,imported_sent=true,legacy_source_key=repeat('b',64)`,
	)
	require.NoError(t, err)
	require.NoError(t, s.QueueReminders(t.Context()))
	notices, err = s.PendingNotifications(t.Context())
	require.NoError(t, err)
	assert.Empty(t, notices)
}

func TestTwoFoodExportsAndCurrentPermission(t *testing.T) {
	t.Parallel()
	s := foodDatabase(t)
	order := mealOrder(t, s, "alice")
	_ = mealOrder(t, s, "bob")
	order, err := s.Execute(t.Context(), "alice", command(order, "begin_payment", legacyfood.Meals))
	require.NoError(t, err)
	proof, err := (orders.Service{DB: s.DB}).UploadProof(t.Context(), "alice", "proof.pdf", []byte("receipt"))
	require.NoError(t, err)
	submit := command(order, "submit_proof", legacyfood.Meals)
	submit.ProofID = proof.ID
	order, err = s.Execute(t.Context(), "alice", submit)
	require.NoError(t, err)
	accept := command(order, "accept", legacyfood.Meals)
	accept.Generation = order.MealPayment.Generation
	_, err = s.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	_, err = s.Export(t.Context(), "alice", "food-event")
	require.Error(t, err)
	exported, err := s.Export(t.Context(), "bob", "food-event")
	require.NoError(t, err)
	details, err := csv.NewReader(strings.NewReader(string(exported.Orders))).ReadAll()
	require.NoError(t, err)
	require.Len(t, details, 3)
	assert.Equal(t, []string{"Да", "Да", "185.00"}, details[1][4:7])
	assert.Equal(t, "Суп", details[1][7])
	summary, err := csv.NewReader(strings.NewReader(string(exported.Summary))).ReadAll()
	require.NoError(t, err)
	require.Len(t, summary, 2)
	assert.Equal(t, []string{"Пятница", "Обед", "Суп", "185.00", "1", "185.00"}, summary[1])
	_, err = s.DB.Exec(t.Context(), `UPDATE core.food_admins SET can_export=false`)
	require.NoError(t, err)
	_, err = s.Export(t.Context(), "bob", "food-event")
	require.Error(t, err)
}
